package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/corsproxy"
	"github.com/QzCurious/seamless-cors/internal/liveconfig"
	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/pacrouting"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

type trafficRuntime struct {
	mu                        sync.RWMutex
	currentSnapshot           liveconfig.Snapshot
	authority                 *userca.Authority
	proxy                     *http.Server
	pacHandler                *pacrouting.DynamicHandler
	pac                       *http.Server
	listeners                 []net.Listener
	liveConfig                *liveconfig.Source
	pacVersion                uint64
	pacUpdates                chan string
	domainListEntriesRevision uint64
}

type serverError struct {
	source string
	err    error
}

func newRuntime(source *liveconfig.Source, snapshot liveconfig.Snapshot) (*trafficRuntime, error) {
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("proxy listener unavailable: %w", err)
	}
	pacListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		proxyListener.Close()
		return nil, fmt.Errorf("PAC listener unavailable: %w", err)
	}

	proxyListen := proxyListener.Addr().String()

	pacBody := pacrouting.Generate(pacrouting.Options{
		ProxyListen: proxyListen,
		CATrusted:   snapshot.CATrusted(),
		DomainList:  snapshot.DomainList(),
	})
	pacHandler := pacrouting.NewDynamicHandler(pacBody)
	return &trafficRuntime{
		currentSnapshot:           snapshot,
		liveConfig:                source,
		pacHandler:                pacHandler,
		proxy:                     &http.Server{},
		pac:                       &http.Server{Handler: pacHandler},
		listeners:                 []net.Listener{proxyListener, pacListener},
		pacVersion:                1,
		pacUpdates:                make(chan string, 1),
		domainListEntriesRevision: snapshot.DomainListEntriesRevision(),
	}, nil
}

func (r *trafficRuntime) SetAuthority(authority *userca.Authority) error {
	r.authority = authority
	proxyHandler, err := corsproxy.New(corsproxy.Options{
		CATrusted: r.currentSnapshot.CATrusted(),
		Authority: authority,
	})
	if err != nil {
		return err
	}
	r.proxy.Handler = proxyHandler
	return nil
}

func (r *trafficRuntime) Serve(ctx context.Context) error {
	return r.ServeReady(ctx, nil)
}

// ServeReady reports when both bound traffic listeners have entered their
// serving goroutines. Callers may then safely publish the PAC URL.
func (r *trafficRuntime) ServeReady(ctx context.Context, ready chan<- struct{}) error {
	if r.proxy.Handler == nil {
		if err := r.SetAuthority(nil); err != nil {
			return err
		}
	}
	errs := make(chan serverError, 3)
	go r.watchLiveConfig(ctx, errs)
	go func() {
		errs <- serverError{source: "proxy", err: r.proxy.Serve(r.listeners[0])}
	}()
	go func() {
		errs <- serverError{source: "pac", err: r.pac.Serve(r.listeners[1])}
	}()
	for _, address := range []string{r.listeners[0].Addr().String(), r.listeners[1].Addr().String()} {
		if err := proveHTTPServing(ctx, address); err != nil {
			_ = r.Close()
			return fmt.Errorf("runtime readiness failed for %s: %w", address, err)
		}
	}
	if ready != nil {
		close(ready)
	}

	select {
	case <-ctx.Done():
		return r.Close()
	case serverErr := <-errs:
		_ = r.Close()
		if serverErr.err == http.ErrServerClosed {
			return nil
		}
		return serverErr.err
	}
}

func proveHTTPServing(ctx context.Context, address string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopCancelClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancelClose()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", address); err != nil {
		return err
	}
	var first [1]byte
	_, err = conn.Read(first[:])
	return err
}

func (r *trafficRuntime) Close() error {
	return r.CloseTraffic()
}

func (r *trafficRuntime) CloseTraffic() error {
	_ = r.proxy.Close()
	return r.pac.Close()
}

func (r *trafficRuntime) PACURL() string {
	r.mu.RLock()
	version := r.pacVersion
	r.mu.RUnlock()
	return r.pacURL(version)
}

func (r *trafficRuntime) PACListen() string {
	return r.listeners[1].Addr().String()
}

func (r *trafficRuntime) PACURLUpdates() <-chan string {
	return r.pacUpdates
}

func (r *trafficRuntime) snapshot() runtimeState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stateLocked()
}

func (r *trafficRuntime) watchLiveConfig(ctx context.Context, errs chan<- serverError) {
	err := r.liveConfig.Watch(ctx, r.applyLiveConfig)
	if err == nil {
		return
	}
	select {
	case errs <- serverError{source: "live-config", err: err}:
	case <-ctx.Done():
	}
}

func (r *trafficRuntime) applyLiveConfig(snapshot liveconfig.Snapshot) {
	r.mu.Lock()
	routingInputsChanged := snapshot.DomainListEntriesRevision() != r.domainListEntriesRevision ||
		snapshot.CATrusted() != r.currentSnapshot.CATrusted()
	r.currentSnapshot = snapshot
	if !routingInputsChanged {
		r.mu.Unlock()
		return
	}
	r.domainListEntriesRevision = snapshot.DomainListEntriesRevision()
	r.pacVersion++
	nextURL := r.pacURL(r.pacVersion)
	r.mu.Unlock()
	r.pacHandler.Set(pacrouting.Generate(pacrouting.Options{
		ProxyListen: r.listeners[0].Addr().String(),
		CATrusted:   snapshot.CATrusted(),
		DomainList:  snapshot.DomainList(),
	}))
	select {
	case r.pacUpdates <- nextURL:
	default:
		select {
		case <-r.pacUpdates:
		default:
		}
		r.pacUpdates <- nextURL
	}
}

func (r *trafficRuntime) pacURL(version uint64) string {
	u := url.URL{
		Scheme:   "http",
		Host:     r.listeners[1].Addr().String(),
		Path:     "/" + managedpac.FootprintFileName,
		RawQuery: fmt.Sprintf("v=%d", version),
	}
	return u.String()
}

type runtimeState struct {
	ProxyListen        string
	PACListen          string
	DomainList         string
	CATrusted          bool
	DomainCount        int
	DomainListWarnings []DomainListWarningDetail
	CATrustPending     bool
}

func (r *trafficRuntime) stateLocked() runtimeState {
	domainList := r.currentSnapshot.DomainList()
	return runtimeState{
		ProxyListen:        r.listeners[0].Addr().String(),
		PACListen:          r.listeners[1].Addr().String(),
		DomainList:         r.currentSnapshot.DomainListPath(),
		CATrusted:          r.currentSnapshot.CATrusted(),
		DomainCount:        len(domainList.HostSelectors) + len(domainList.OriginSelectors),
		DomainListWarnings: domainListWarningDetails(domainList.Warnings),
		CATrustPending:     r.currentSnapshot.CATrustPending(),
	}
}

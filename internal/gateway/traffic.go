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
	mu                          sync.RWMutex
	currentSnapshot             liveconfig.Snapshot
	authority                   *userca.Authority
	caAdmissionGuard            *sync.Mutex
	loadUsableAuthority         usableAuthorityLoader
	proxyHandler                *dynamicHTTPHandler
	proxyConfigured             bool
	caTrustWarning              string
	proxy                       *http.Server
	pacHandler                  *pacrouting.DynamicHandler
	pac                         *http.Server
	listeners                   []net.Listener
	liveConfig                  *liveconfig.Config
	pacVersion                  uint64
	pacUpdates                  chan string
	upstreamListEntriesRevision uint64
}

type usableAuthorityLoader func(context.Context) (*userca.Authority, userca.Report, error)

type trustedHTTPSAdmission struct {
	guard      *sync.Mutex
	loadUsable usableAuthorityLoader
}

type dynamicHTTPHandler struct {
	mu      sync.RWMutex
	current http.Handler
}

func (h *dynamicHTTPHandler) Set(next http.Handler) {
	h.mu.Lock()
	h.current = next
	h.mu.Unlock()
}

func (h *dynamicHTTPHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.mu.RLock()
	current := h.current
	h.mu.RUnlock()
	current.ServeHTTP(w, req)
}

type serverError struct {
	source string
	err    error
}

func newRuntime(config *liveconfig.Config, snapshot liveconfig.Snapshot, admission trustedHTTPSAdmission) (*trafficRuntime, error) {
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
		ProxyListen:  proxyListen,
		CATrusted:    snapshot.CATrusted(),
		UpstreamList: snapshot.UpstreamList(),
	})
	pacHandler := pacrouting.NewDynamicHandler(pacBody)
	proxyHandler := &dynamicHTTPHandler{current: http.NotFoundHandler()}
	if admission.guard == nil {
		admission.guard = &sync.Mutex{}
	}
	return &trafficRuntime{
		currentSnapshot:             snapshot,
		liveConfig:                  config,
		caAdmissionGuard:            admission.guard,
		loadUsableAuthority:         admission.loadUsable,
		proxyHandler:                proxyHandler,
		pacHandler:                  pacHandler,
		proxy:                       &http.Server{Handler: proxyHandler},
		pac:                         &http.Server{Handler: pacHandler},
		listeners:                   []net.Listener{proxyListener, pacListener},
		pacVersion:                  1,
		pacUpdates:                  make(chan string, 1),
		upstreamListEntriesRevision: snapshot.UpstreamListEntriesRevision(),
	}, nil
}

func (r *trafficRuntime) SetAuthority(authority *userca.Authority) error {
	if !r.currentSnapshot.CATrusted() {
		authority = nil
	}
	proxyHandler, err := corsproxy.New(corsproxy.Options{
		CATrusted: r.currentSnapshot.CATrusted(),
		Authority: authority,
	})
	if err != nil {
		return err
	}
	r.proxyHandler.Set(proxyHandler)
	r.mu.Lock()
	r.authority = authority
	r.proxyConfigured = true
	r.caTrustWarning = ""
	r.mu.Unlock()
	return nil
}

func (r *trafficRuntime) Serve(ctx context.Context) error {
	return r.ServeReady(ctx, nil)
}

// ServeReady reports when both bound traffic listeners have entered their
// serving goroutines. Callers may then safely publish the PAC URL.
func (r *trafficRuntime) ServeReady(ctx context.Context, ready chan<- struct{}) error {
	if !r.proxyConfigured {
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
	err := r.liveConfig.Observe(ctx, func(snapshot liveconfig.Snapshot) {
		r.applyLiveConfig(ctx, snapshot)
	})
	if err == nil {
		return
	}
	select {
	case errs <- serverError{source: "live-config", err: err}:
	case <-ctx.Done():
	}
}

func (r *trafficRuntime) applyLiveConfig(ctx context.Context, snapshot liveconfig.Snapshot) {
	r.mu.RLock()
	previous := r.currentSnapshot
	previousTrustWarning := r.caTrustWarning
	previousAuthority := r.authority
	r.mu.RUnlock()

	caTrustWarning := previousTrustWarning
	authority := previousAuthority
	previousTrustActive := trustedHTTPSActive(previous, previousAuthority)
	applyTrust := snapshot.CATrusted() != previous.CATrusted() ||
		(snapshot.CATrusted() && !previousTrustActive)
	if applyTrust {
		r.caAdmissionGuard.Lock()
		proxyHandler, nextAuthority, warning := r.liveProxyHandler(ctx, snapshot.CATrusted())
		r.proxyHandler.Set(proxyHandler)
		authority = nextAuthority
		caTrustWarning = warning
	}

	nextTrustActive := trustedHTTPSActive(snapshot, authority)
	r.mu.Lock()
	routingInputsChanged := snapshot.UpstreamListEntriesRevision() != r.upstreamListEntriesRevision ||
		nextTrustActive != previousTrustActive
	r.currentSnapshot = snapshot
	r.authority = authority
	r.caTrustWarning = caTrustWarning
	if !routingInputsChanged {
		r.mu.Unlock()
		if applyTrust {
			r.caAdmissionGuard.Unlock()
		}
		return
	}
	r.upstreamListEntriesRevision = snapshot.UpstreamListEntriesRevision()
	r.pacVersion++
	nextURL := r.pacURL(r.pacVersion)
	r.mu.Unlock()
	if applyTrust {
		r.caAdmissionGuard.Unlock()
	}
	r.pacHandler.Set(pacrouting.Generate(pacrouting.Options{
		ProxyListen:  r.listeners[0].Addr().String(),
		CATrusted:    nextTrustActive,
		UpstreamList: snapshot.UpstreamList(),
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

func trustedHTTPSActive(snapshot liveconfig.Snapshot, authority *userca.Authority) bool {
	return snapshot.CATrusted() && authority != nil
}

func (r *trafficRuntime) liveProxyHandler(ctx context.Context, configured bool) (http.Handler, *userca.Authority, string) {
	if !configured {
		handler, _ := corsproxy.New(corsproxy.Options{})
		return handler, nil, ""
	}
	if r.loadUsableAuthority == nil {
		handler, _ := corsproxy.New(corsproxy.Options{})
		return handler, nil, "ca-trusted is configured, but no Installed User CA loader is available"
	}
	authority, report, err := r.loadUsableAuthority(ctx)
	if err != nil {
		handler, _ := corsproxy.New(corsproxy.Options{})
		return handler, nil, fmt.Sprintf("ca-trusted is configured, but the Installed User CA could not be inspected: %v", err)
	}
	if authority == nil {
		handler, _ := corsproxy.New(corsproxy.Options{})
		return handler, nil, fmt.Sprintf("ca-trusted is configured, but the Installed User CA is %s", report.Health)
	}
	handler, err := corsproxy.New(corsproxy.Options{
		CATrusted: true,
		Authority: authority,
	})
	if err != nil {
		fallback, _ := corsproxy.New(corsproxy.Options{})
		return fallback, nil, fmt.Sprintf("ca-trusted is configured, but trusted HTTPS interception could not be activated: %v", err)
	}
	return handler, authority, ""
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
	ProxyListen          string
	PACListen            string
	UpstreamList         string
	CATrusted            bool
	TrustedHTTPSActive   bool
	CATrustWarning       string
	UpstreamCount        int
	UpstreamListWarnings []UpstreamListWarningDetail
}

func (r *trafficRuntime) stateLocked() runtimeState {
	upstreamList := r.currentSnapshot.UpstreamList()
	return runtimeState{
		ProxyListen:          r.listeners[0].Addr().String(),
		PACListen:            r.listeners[1].Addr().String(),
		UpstreamList:         r.currentSnapshot.UpstreamListPath(),
		CATrusted:            r.currentSnapshot.CATrusted(),
		TrustedHTTPSActive:   trustedHTTPSActive(r.currentSnapshot, r.authority),
		CATrustWarning:       r.caTrustWarning,
		UpstreamCount:        len(upstreamList.HostSelectors) + len(upstreamList.OriginSelectors),
		UpstreamListWarnings: upstreamListWarningDetails(upstreamList.Warnings),
	}
}

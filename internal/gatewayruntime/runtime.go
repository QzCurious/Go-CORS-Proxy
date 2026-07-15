package gatewayruntime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"

	"seamless-cors/internal/corsproxy"
	"seamless-cors/internal/liveconfig"
	"seamless-cors/internal/pacrouting"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

type Runtime struct {
	mu         sync.RWMutex
	live       liveconfig.Config
	authority  *userca.Authority
	proxy      *http.Server
	pacHandler *pacrouting.DynamicHandler
	pac        *http.Server
	listeners  []net.Listener
	liveConfig *liveconfig.Source
	pacVersion uint64
	pacUpdates chan string
	routeSet   string
}

type serverError struct {
	source string
	err    error
}

func New(source *liveconfig.Source, live liveconfig.Config) (*Runtime, error) {
	entries := live.Entries()
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

	pacBody := pacrouting.Generate(pacrouting.Options{ProxyListen: proxyListen, CATrusted: live.CATrusted(), Entries: entries})
	pacHandler := pacrouting.NewDynamicHandler(pacBody)
	return &Runtime{
		live:       live,
		liveConfig: source,
		pacHandler: pacHandler,
		proxy:      &http.Server{},
		pac:        &http.Server{Handler: pacHandler},
		listeners:  []net.Listener{proxyListener, pacListener},
		pacVersion: 1,
		pacUpdates: make(chan string, 1),
		routeSet:   pacrouting.RouteSetFingerprint(entries, live.CATrusted()),
	}, nil
}

func (r *Runtime) SetAuthority(authority *userca.Authority) error {
	r.authority = authority
	proxyHandler, err := corsproxy.New(corsproxy.Options{
		CATrusted: r.live.CATrusted(),
		Authority: authority,
	})
	if err != nil {
		return err
	}
	r.proxy.Handler = proxyHandler
	return nil
}

func (r *Runtime) Serve(ctx context.Context) error {
	if r.proxy.Handler == nil {
		if err := r.SetAuthority(nil); err != nil {
			return err
		}
	}
	errs := make(chan serverError, 3)
	go r.watchLiveConfig(ctx, errs)
	go func() { errs <- serverError{source: "proxy", err: r.proxy.Serve(r.listeners[0])} }()
	go func() { errs <- serverError{source: "pac", err: r.pac.Serve(r.listeners[1])} }()

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

func (r *Runtime) Close() error {
	return r.CloseTraffic()
}

func (r *Runtime) CloseTraffic() error {
	_ = r.proxy.Close()
	return r.pac.Close()
}

func (r *Runtime) PACURL() string {
	r.mu.RLock()
	version := r.pacVersion
	r.mu.RUnlock()
	return r.pacURL(version)
}

func (r *Runtime) PACListen() string {
	return r.listeners[1].Addr().String()
}

func (r *Runtime) PACURLUpdates() <-chan string {
	return r.pacUpdates
}

func (r *Runtime) State() State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stateLocked()
}

func (r *Runtime) watchLiveConfig(ctx context.Context, errs chan<- serverError) {
	events := r.liveConfig.Watch(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.Err != nil {
				select {
				case errs <- serverError{source: "live-config", err: event.Err}:
				case <-ctx.Done():
				}
				return
			}
			r.applyLiveConfig(event.Config)
		}
	}
}

func (r *Runtime) applyLiveConfig(live liveconfig.Config) {
	nextRouteSet := pacrouting.RouteSetFingerprint(live.Entries(), live.CATrusted())
	r.mu.Lock()
	r.live = live
	if nextRouteSet == r.routeSet {
		r.mu.Unlock()
		return
	}
	r.routeSet = nextRouteSet
	r.pacVersion++
	nextURL := r.pacURL(r.pacVersion)
	r.mu.Unlock()
	r.pacHandler.Set(pacrouting.Generate(pacrouting.Options{
		ProxyListen: r.listeners[0].Addr().String(),
		CATrusted:   live.CATrusted(),
		Entries:     live.Entries(),
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

func (r *Runtime) pacURL(version uint64) string {
	u := url.URL{
		Scheme:   "http",
		Host:     r.listeners[1].Addr().String(),
		Path:     "/" + platform.PACFootprintFileName,
		RawQuery: fmt.Sprintf("v=%d", version),
	}
	return u.String()
}

type State struct {
	ProxyListen      string
	PACListen        string
	DomainList       string
	CATrusted        bool
	DomainCount      int
	PendingLifecycle []string
}

func (r *Runtime) stateLocked() State {
	entries := r.live.Entries()
	return State{
		ProxyListen:      r.listeners[0].Addr().String(),
		PACListen:        r.listeners[1].Addr().String(),
		DomainList:       r.live.DomainListPath(),
		CATrusted:        r.live.CATrusted(),
		DomainCount:      len(entries),
		PendingLifecycle: r.live.PendingLifecycle(),
	}
}

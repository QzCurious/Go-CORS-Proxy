package pacrouting

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

// Routing owns the generated PAC artifact and the handler that serves it.
// Gateway Runtime supplies only semantic selectors and whether Trusted
// HTTPS Interception is currently active; selector interpretation and PAC
// rendering remain inside this module.
type Routing struct {
	mu           sync.RWMutex
	proxyListen  string
	trustedHTTPS bool
	routes       routeSet
	handler      *dynamicHandler
}

// NewRouting creates PAC Routing with an empty route set. The returned handler
// is ready to serve immediately; the first Apply call replaces its body when
// the supplied semantic route set differs from the empty set.
func NewRouting(proxyListen string) *Routing {
	routes := deriveRouteSet(nil, nil, false)
	return &Routing{
		proxyListen: proxyListen,
		routes:      routes,
		handler:     newDynamicHandler(render(proxyListen, routes)),
	}
}

// Apply adopts the newest semantic routing input. It returns true only when
// the generated PAC route set changed; equivalent input or trust changes that
// do not affect any route are coalesced without publishing a new body.
func (r *Routing) Apply(hostSelectors []upstreamlist.HostSelector, originSelectors []upstreamlist.OriginSelector, trustedHTTPS bool) bool {
	nextRoutes := deriveRouteSet(hostSelectors, originSelectors, trustedHTTPS)
	r.mu.Lock()
	defer r.mu.Unlock()
	if sameRouteSet(r.routes, nextRoutes) {
		r.trustedHTTPS = trustedHTTPS
		return false
	}
	r.trustedHTTPS = trustedHTTPS
	r.routes = nextRoutes
	r.handler.Set(render(r.proxyListen, nextRoutes))
	return true
}

// Render derives the complete effective PAC for semantic routing input. The
// caller supplies the proxy endpoint because PAC Routing does not own the
// runtime listener.
func Render(proxyListen string, hostSelectors []upstreamlist.HostSelector, originSelectors []upstreamlist.OriginSelector, trustedHTTPS bool) string {
	return render(proxyListen, deriveRouteSet(hostSelectors, originSelectors, trustedHTTPS))
}

// Handler returns the dynamic HTTP handler owned by PAC Routing.
func (r *Routing) Handler() http.Handler {
	r.mu.RLock()
	handler := r.handler
	r.mu.RUnlock()
	return handler
}

// Body returns the current generated PAC text. Gateway Runtime normally only
// needs Handler; this accessor is useful for focused diagnostics and tests.
func (r *Routing) Body() string {
	r.mu.RLock()
	routes := r.routes
	proxyListen := r.proxyListen
	r.mu.RUnlock()
	return render(proxyListen, routes)
}

// TrustedHTTPS reports the latest Trusted HTTPS Interception state adopted by
// Apply.
func (r *Routing) TrustedHTTPS() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.trustedHTTPS
}

//go:embed proxy.pac.js
var pacProgram string

type routeSet struct {
	hostRoutes   []hostRoute
	originRoutes []string
}

func deriveRouteSet(hostSelectors []upstreamlist.HostSelector, originSelectors []upstreamlist.OriginSelector, trustedHTTPS bool) routeSet {
	return routeSet{
		hostRoutes:   deriveHostRoutes(hostSelectors, trustedHTTPS),
		originRoutes: deriveOriginRoutes(originSelectors, trustedHTTPS),
	}
}

func sameRouteSet(left, right routeSet) bool {
	return sameHostRouteSet(left.hostRoutes, right.hostRoutes) &&
		sameStringSet(left.originRoutes, right.originRoutes)
}

func sameHostRouteSet(left, right []hostRoute) bool {
	leftSet := make(map[hostRoute]struct{}, len(left))
	for _, route := range left {
		leftSet[route] = struct{}{}
	}
	rightSet := make(map[hostRoute]struct{}, len(right))
	for _, route := range right {
		rightSet[route] = struct{}{}
	}
	return sameMapKeys(leftSet, rightSet)
}

func sameStringSet(left, right []string) bool {
	leftSet := make(map[string]struct{}, len(left))
	for _, route := range left {
		leftSet[route] = struct{}{}
	}
	rightSet := make(map[string]struct{}, len(right))
	for _, route := range right {
		rightSet[route] = struct{}{}
	}
	return sameMapKeys(leftSet, rightSet)
}

func sameMapKeys[K comparable](left, right map[K]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, ok := right[key]; !ok {
			return false
		}
	}
	return true
}

func render(proxyListen string, routes routeSet) string {
	config := struct {
		Proxy        string      `json:"Proxy"`
		HostRoutes   []hostRoute `json:"HostRoutes"`
		OriginRoutes []string    `json:"OriginRoutes"`
	}{
		Proxy:        proxyListen,
		HostRoutes:   routes.hostRoutes,
		OriginRoutes: routes.originRoutes,
	}

	data, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return "var VIEW_BAG = " + string(data) + ";\n\n" + pacProgram
}

type hostRoute struct {
	Scheme        string `json:"Scheme"`
	Hostname      string `json:"Hostname"`
	HostnameMatch string `json:"HostnameMatch"`
}

// deriveHostRoutes expands every Host Selector into its active HTTP(S)
// routes. HTTP is always active; HTTPS requires trusted interception.
func deriveHostRoutes(selectors []upstreamlist.HostSelector, caTrusted bool) []hostRoute {
	routes := make([]hostRoute, 0, len(selectors)*2)
	for _, selector := range selectors {
		routes = append(routes, hostRouteFromSelector(selector, "http"))
		if caTrusted {
			routes = append(routes, hostRouteFromSelector(selector, "https"))
		}
	}
	return routes
}

func hostRouteFromSelector(selector upstreamlist.HostSelector, scheme string) hostRoute {
	return hostRoute{
		Scheme:        scheme,
		Hostname:      selector.Hostname,
		HostnameMatch: serializedHostnameMatch(selector.HostnameMatch),
	}
}

func serializedHostnameMatch(match upstreamlist.HostnameMatch) string {
	switch match {
	case upstreamlist.HostnameSingleLevel:
		return "SingleLevel"
	case upstreamlist.HostnameRecursive:
		return "Recursive"
	default:
		return "Exact"
	}
}

// deriveOriginRoutes expands each active Origin Selector into exact PAC URL
// representations. An omitted or default port gets both implicit and explicit
// forms.
func deriveOriginRoutes(selectors []upstreamlist.OriginSelector, caTrusted bool) []string {
	routes := make([]string, 0, len(selectors)*2)
	seen := make(map[string]struct{}, len(selectors)*2)
	appendRoute := func(route string) {
		if _, ok := seen[route]; ok {
			return
		}
		seen[route] = struct{}{}
		routes = append(routes, route)
	}
	for _, selector := range selectors {
		if selector.Scheme == "https" && !caTrusted {
			continue
		}

		origin := selector.Scheme + "://" + originHostname(selector.Hostname)
		defaultPort := originDefaultPort(selector.Scheme)
		if selector.Port == "" || selector.Port == defaultPort {
			appendRoute(origin)
			appendRoute(origin + ":" + defaultPort)
			continue
		}
		appendRoute(origin + ":" + selector.Port)
	}
	return routes
}

func originDefaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

func originHostname(hostname string) string {
	if strings.Contains(hostname, ":") {
		return "[" + hostname + "]"
	}
	return hostname
}

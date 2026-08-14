package pacrouting

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

// Projection is the complete immutable PAC Routing interpretation used by the
// live PAC endpoint and Managed PAC publication.
type Projection struct {
	proxyListen string
	pacListen   string
	routes      routeSet
	body        string
}

// Project derives a PAC Projection from complete Gateway-owned input.
func Project(upstreams upstreamlist.Projection, trustedHTTPS bool, proxyListen, pacListen string) Projection {
	routes := deriveRouteSet(upstreams.HostSelectors, upstreams.OriginSelectors, trustedHTTPS)
	return Projection{
		proxyListen: proxyListen,
		pacListen:   pacListen,
		routes:      routes,
		body:        render(proxyListen, routes),
	}
}

// Equal reports PAC Projection identity. Route ordering is not significant;
// both runtime endpoints are part of identity.
func Equal(left, right Projection) bool {
	return left.proxyListen == right.proxyListen &&
		left.pacListen == right.pacListen &&
		sameRouteSet(left.routes, right.routes)
}

func (p Projection) Body() string      { return p.body }
func (p Projection) PACListen() string { return p.pacListen }

// Handler serves this immutable PAC Projection.
func (p Projection) Handler() http.Handler { return staticHandler{body: p.body} }

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

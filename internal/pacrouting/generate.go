package pacrouting

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

type Options struct {
	ProxyListen  string
	CATrusted    bool
	UpstreamList upstreamlist.UpstreamList
}

//go:embed proxy.pac.js
var pacProgram string

func Generate(opts Options) string {
	config := struct {
		Proxy        string      `json:"Proxy"`
		HostRoutes   []hostRoute `json:"HostRoutes"`
		OriginRoutes []string    `json:"OriginRoutes"`
	}{
		Proxy:        opts.ProxyListen,
		HostRoutes:   deriveHostRoutes(opts.UpstreamList.HostSelectors, opts.CATrusted),
		OriginRoutes: deriveOriginRoutes(opts.UpstreamList.OriginSelectors, opts.CATrusted),
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

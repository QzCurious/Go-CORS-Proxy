package pacrouting

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/QzCurious/seamless-cors/internal/domainlist"
)

type Options struct {
	ProxyListen string
	CATrusted   bool
	DomainList  domainlist.DomainList
}

//go:embed proxy.pac.js
var pacProgram string

func Generate(opts Options) string {
	config := struct {
		Proxy        string        `json:"Proxy"`
		DomainRoutes []domainRoute `json:"DomainRoutes"`
		OriginRoutes []string      `json:"OriginRoutes"`
	}{
		Proxy:        opts.ProxyListen,
		DomainRoutes: deriveDomainRoutes(opts.DomainList.DomainSelectors, opts.CATrusted),
		OriginRoutes: deriveOriginRoutes(opts.DomainList.OriginSelectors, opts.CATrusted),
	}

	data, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return "var VIEW_BAG = " + string(data) + ";\n\n" + pacProgram
}

type domainRoute struct {
	Scheme        string `json:"Scheme"`
	Hostname      string `json:"Hostname"`
	HostnameMatch string `json:"HostnameMatch"`
}

// deriveDomainRoutes expands every Domain Selector into its active HTTP(S)
// routes. HTTP is always active; HTTPS requires trusted interception.
func deriveDomainRoutes(selectors []domainlist.DomainSelector, caTrusted bool) []domainRoute {
	routes := make([]domainRoute, 0, len(selectors)*2)
	for _, selector := range selectors {
		routes = append(routes, domainRouteFromSelector(selector, "http"))
		if caTrusted {
			routes = append(routes, domainRouteFromSelector(selector, "https"))
		}
	}
	return routes
}

func domainRouteFromSelector(selector domainlist.DomainSelector, scheme string) domainRoute {
	return domainRoute{
		Scheme:        scheme,
		Hostname:      selector.Hostname,
		HostnameMatch: serializedHostnameMatch(selector.HostnameMatch),
	}
}

func serializedHostnameMatch(match domainlist.HostnameMatch) string {
	switch match {
	case domainlist.HostnameSingleLevel:
		return "SingleLevel"
	case domainlist.HostnameRecursive:
		return "Recursive"
	default:
		return "Exact"
	}
}

// deriveOriginRoutes expands each active Origin Selector into exact PAC URL
// representations. An omitted or default port gets both implicit and explicit
// forms.
func deriveOriginRoutes(selectors []domainlist.OriginSelector, caTrusted bool) []string {
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

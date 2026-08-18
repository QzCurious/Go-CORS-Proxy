package pacrouting

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"net/http"
	"slices"

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

// Equal reports PAC Projection identity. Both runtime endpoints are part of
// identity.
func Equal(left, right Projection) bool {
	return left.proxyListen == right.proxyListen &&
		left.pacListen == right.pacListen &&
		slices.Equal(left.routes, right.routes)
}

func (p Projection) Body() string      { return p.body }
func (p Projection) PACListen() string { return p.pacListen }

// Handler serves this immutable PAC Projection.
func (p Projection) Handler() http.Handler { return staticHandler{body: p.body} }

//go:embed proxy.pac.js
var pacProgram string

type routeSet []pacRoute

func deriveRouteSet(hostSelectors []upstreamlist.HostSelector, originSelectors []upstreamlist.OriginSelector, trustedHTTPS bool) routeSet {
	routes := make(routeSet, 0, len(hostSelectors)*2+len(originSelectors))

	for _, selector := range hostSelectors {
		routes = append(routes, pacRoute{
			Scheme:   "http",
			Hostname: selector.Hostname,
			Wildcard: selector.Wildcard,
		})
		if trustedHTTPS {
			routes = append(routes, pacRoute{
				Scheme:   "https",
				Hostname: selector.Hostname,
				Wildcard: selector.Wildcard,
			})
		}
	}

	for _, selector := range originSelectors {
		if selector.Scheme == "https" && !trustedHTTPS {
			continue
		}
		port := selector.Port
		if port == "" {
			if selector.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		routes = append(routes, pacRoute{
			Scheme:   selector.Scheme,
			Hostname: selector.Hostname,
			Port:     port,
		})
	}

	seen := make(map[pacRoute]struct{}, len(routes))
	for _, route := range routes {
		seen[route] = struct{}{}
	}

	routes = routes[:0]
	for route := range seen {
		routes = append(routes, route)
	}
	slices.SortFunc(routes, comparePACRoutes)
	return routes
}

func render(proxyListen string, routes routeSet) string {
	config := struct {
		Proxy  string   `json:"proxy"`
		Routes routeSet `json:"routes"`
	}{
		Proxy:  proxyListen,
		Routes: routes,
	}

	data, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return "var VIEW_BAG = " + string(data) + ";\n\n" + pacProgram
}

type pacRoute struct {
	Scheme   string
	Hostname string
	// Port is empty for an any-port route and otherwise contains one normalized
	// effective port.
	Port     string
	Wildcard bool
}

func (r pacRoute) MarshalJSON() ([]byte, error) {
	var port *string
	if r.Port != "" {
		port = &r.Port
	}
	return json.Marshal(struct {
		Scheme   string  `json:"scheme"`
		Hostname string  `json:"hostname"`
		Port     *string `json:"port"`
		Wildcard bool    `json:"wildcard"`
	}{
		Scheme:   r.Scheme,
		Hostname: r.Hostname,
		Port:     port,
		Wildcard: r.Wildcard,
	})
}

func comparePACRoutes(left, right pacRoute) int {
	if result := cmp.Compare(left.Scheme, right.Scheme); result != 0 {
		return result
	}
	if result := cmp.Compare(left.Hostname, right.Hostname); result != 0 {
		return result
	}
	if result := cmp.Compare(left.Port, right.Port); result != 0 {
		return result
	}
	if left.Wildcard == right.Wildcard {
		return 0
	}
	if left.Wildcard {
		return 1
	}
	return -1
}

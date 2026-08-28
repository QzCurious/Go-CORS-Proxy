package pacrouting

import (
	_ "embed"
	"encoding/json"
	"strconv"

	"github.com/QzCurious/seamless-cors/internal/httpsfacade"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

//go:embed proxy.pac.js
var pacProgram string

type pacRoute struct {
	Scheme   string  `json:"scheme"`
	Hostname string  `json:"hostname"`
	Port     *string `json:"port"`
	Wildcard bool    `json:"wildcard"`
}

// Project renders PAC content from complete Gateway-owned routing input. The
// supplied HTTPS Facade Projection must have been formed from upstreams.
func Project(
	upstreams upstreamlist.Projection,
	facades httpsfacade.Projection,
	trustedHTTPS bool,
	proxyListen string,
) string {
	routes := make(
		[]pacRoute,
		0,
		len(upstreams.HostSelectors)*2+len(upstreams.OriginSelectors)+len(facades.Routes()),
	)

	for _, selector := range upstreams.HostSelectors {
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

	for _, selector := range upstreams.OriginSelectors {
		if selector.Scheme == "https" && !trustedHTTPS {
			continue
		}
		port := strconv.FormatUint(uint64(selector.Port), 10)
		routes = append(routes, pacRoute{
			Scheme:   selector.Scheme,
			Hostname: selector.Hostname,
			Port:     &port,
		})
	}
	if trustedHTTPS {
		for _, facade := range facades.Routes() {
			port := strconv.FormatUint(uint64(facade.HTTPSPort), 10)
			routes = append(routes, pacRoute{
				Scheme:   "https",
				Hostname: facade.Hostname,
				Port:     &port,
			})
		}
	}

	config := struct {
		Proxy  string     `json:"proxy"`
		Routes []pacRoute `json:"routes"`
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

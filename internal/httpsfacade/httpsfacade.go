// Package httpsfacade projects HTTP Origin Selectors into browser-facing HTTPS
// routes and adapts matching requests and redirects at the proxy seam.
package httpsfacade

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

const (
	httpDefaultPort  = uint16(80)
	httpsDefaultPort = uint16(443)
)

type origin struct {
	hostname string
	port     uint16
}

// Route maps one exact browser-facing HTTPS origin to its selected HTTP
// upstream. Values are immutable after projection.
type Route struct {
	Hostname  string
	HTTPSPort uint16
	HTTPPort  uint16
}

// Projection is the complete immutable HTTPS Facade interpretation of one
// Origin Selector snapshot. Its zero value is empty.
type Projection struct {
	routes          []Route
	byBrowserOrigin map[origin]int
}

// Project derives HTTPS Facade routes and resolves exact-origin specificity.
// Origin Selectors must satisfy the upstreamlist projection postconditions.
func Project(selectors []upstreamlist.OriginSelector) Projection {
	nativeHTTPS := make(map[origin]struct{}, len(selectors))
	for _, selector := range selectors {
		if selector.Port == 0 {
			panic("httpsfacade: Origin Selector port is required")
		}
		switch selector.Scheme {
		case "http":
		case "https":
			nativeHTTPS[origin{hostname: selector.Hostname, port: selector.Port}] = struct{}{}
		default:
			panic("httpsfacade: Origin Selector scheme must be HTTP(S)")
		}
	}

	routes := make([]Route, 0, len(selectors))
	byBrowserOrigin := make(map[origin]int, len(selectors))
	for _, selector := range selectors {
		if selector.Scheme != "http" {
			continue
		}
		httpsPort := selector.Port
		if selector.Port == httpDefaultPort {
			httpsPort = httpsDefaultPort
		}
		browserOrigin := origin{hostname: selector.Hostname, port: httpsPort}
		if _, shadowed := nativeHTTPS[browserOrigin]; shadowed {
			continue
		}
		candidate := Route{
			Hostname:  selector.Hostname,
			HTTPSPort: httpsPort,
			HTTPPort:  selector.Port,
		}
		if index, collision := byBrowserOrigin[browserOrigin]; collision {
			// An unchanged-port facade is more specific than the special
			// HTTP-port-80 to HTTPS-port-443 translation.
			if candidate.HTTPPort == candidate.HTTPSPort &&
				routes[index].HTTPPort != routes[index].HTTPSPort {
				routes[index] = candidate
			}
			continue
		}
		byBrowserOrigin[browserOrigin] = len(routes)
		routes = append(routes, candidate)
	}
	if len(routes) == 0 {
		return Projection{}
	}
	return Projection{routes: routes, byBrowserOrigin: byBrowserOrigin}
}

// Routes returns the projection's immutable route values in stable selector
// order. Callers treat the returned slice as read-only.
func (p Projection) Routes() []Route {
	return p.routes
}

func (p Projection) resolve(hostname string, port uint16) (Route, bool) {
	index, ok := p.byBrowserOrigin[origin{
		hostname: strings.ToLower(hostname),
		port:     port,
	}]
	if !ok {
		return Route{}, false
	}
	return p.routes[index], true
}

// Live owns the atomically published current Projection shared by Proxy
// generations. Set takes effect at the next request lookup.
type Live struct {
	current atomic.Pointer[Projection]
}

func NewLive(initial Projection) *Live {
	live := &Live{}
	live.Set(initial)
	return live
}

// Set publishes one complete immutable Projection.
func (l *Live) Set(next Projection) {
	l.current.Store(&next)
}

// Forward adapts a matching browser HTTPS request to its selected HTTP origin
// and returns the admitting Route for response adaptation.
func (l *Live) Forward(req *http.Request) (Route, bool) {
	if req == nil || req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") {
		return Route{}, false
	}
	port, ok := effectivePort(req.URL)
	if !ok {
		return Route{}, false
	}
	route, ok := l.current.Load().resolve(req.URL.Hostname(), port)
	if !ok {
		return Route{}, false
	}

	req.URL.Scheme = "http"
	req.URL.Host = authority(route.Hostname, route.HTTPPort, httpDefaultPort)
	req.Host = req.URL.Host
	return route, true
}

// RewriteResponse adapts same-upstream absolute redirects back to the
// browser-facing HTTPS origin. Other response state remains untouched.
func (r Route) RewriteResponse(resp *http.Response) {
	if resp == nil {
		return
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return
	}
	target, err := url.Parse(location)
	if err != nil || !target.IsAbs() || target.Host == "" || !strings.EqualFold(target.Scheme, "http") {
		return
	}
	port, ok := effectivePort(target)
	if !ok || !strings.EqualFold(target.Hostname(), r.Hostname) || port != r.HTTPPort {
		return
	}
	target.Scheme = "https"
	target.Host = authority(r.Hostname, r.HTTPSPort, httpsDefaultPort)
	resp.Header.Set("Location", target.String())
}

func effectivePort(target *url.URL) (uint16, bool) {
	if port := target.Port(); port != "" {
		parsed, err := strconv.ParseUint(port, 10, 16)
		if err != nil || parsed == 0 {
			return 0, false
		}
		return uint16(parsed), true
	}
	switch strings.ToLower(target.Scheme) {
	case "http":
		return httpDefaultPort, true
	case "https":
		return httpsDefaultPort, true
	default:
		return 0, false
	}
}

func authority(hostname string, port, defaultPort uint16) string {
	if port != defaultPort {
		return net.JoinHostPort(hostname, strconv.FormatUint(uint64(port), 10))
	}
	if strings.Contains(hostname, ":") {
		return "[" + hostname + "]"
	}
	return hostname
}

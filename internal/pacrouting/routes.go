package pacrouting

import (
	"net/http"
	"sync/atomic"

	"seamless-cors/internal/liveconfig"
)

func Handler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
}

type DynamicHandler struct {
	body atomic.Value
}

func NewDynamicHandler(body string) *DynamicHandler {
	h := &DynamicHandler{}
	h.Set(body)
	return h
}

func (h *DynamicHandler) Set(body string) {
	h.body.Store(body)
}

func (h *DynamicHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.body.Load().(string)))
}

type route struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   string `json:"port"`
	Match  string `json:"match"`
}

func deriveRoutes(entries []liveconfig.DomainListEntry, caTrusted bool) []route {
	routes := []route{}
	for _, entry := range entries {
		if entry.Scheme == "" {
			routes = append(routes, routeFromEntry(entry, "http"))
			if caTrusted {
				routes = append(routes, routeFromEntry(entry, "https"))
			}
			continue
		}
		if entry.Scheme == "https" && !caTrusted {
			continue
		}
		routes = append(routes, routeFromEntry(entry, entry.Scheme))
	}
	return routes
}

func routeFromEntry(entry liveconfig.DomainListEntry, scheme string) route {
	port := entry.Port
	if entry.Scheme != "" && port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return route{
		Scheme: scheme,
		Host:   entry.Hostname,
		Port:   port,
		Match:  routeMatch(entry.HostMatch),
	}
}

func routeMatch(match liveconfig.HostMatch) string {
	switch match {
	case liveconfig.HostSingleLevel:
		return "single"
	case liveconfig.HostRecursive:
		return "recursive"
	default:
		return "exact"
	}
}

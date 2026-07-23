package pacrouting

import (
	"net/http"
	"strings"
	"sync/atomic"

	"seamless-cors/internal/domainlist"
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

type routeBuckets struct {
	exactHosts      []hostRoute
	wildcardParents []hostRoute
	origins         []originRoute
}

type hostRoute struct {
	Host       string `json:"host"`
	AllowHTTP  bool   `json:"http"`
	AllowHTTPS bool   `json:"https"`
}

type originRoute struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   string `json:"port"`
}

func deriveRouteBuckets(entries []domainlist.Entry, caTrusted bool) routeBuckets {
	buckets := routeBuckets{
		exactHosts:      []hostRoute{},
		wildcardParents: []hostRoute{},
		origins:         []originRoute{},
	}
	for _, entry := range entries {
		if entry.Scheme != "" {
			if entry.Scheme == "https" && !caTrusted {
				continue
			}
			buckets.origins = append(buckets.origins, originRoute{
				Scheme: entry.Scheme,
				Host:   entry.Host,
				Port:   entry.Port,
			})
			continue
		}
		route := hostRoute{
			Host:       entry.Host,
			AllowHTTP:  true,
			AllowHTTPS: caTrusted,
		}
		if entry.Wildcard {
			route.Host = strings.TrimPrefix(entry.Host, "*.")
			buckets.wildcardParents = append(buckets.wildcardParents, route)
			continue
		}
		buckets.exactHosts = append(buckets.exactHosts, route)
	}
	return buckets
}

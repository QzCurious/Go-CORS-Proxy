package pacrouting

import (
	"net/http"
	"sync/atomic"
)

// LiveHandler serves the latest Projection adopted by Gateway.
type LiveHandler struct {
	body atomic.Value
}

func NewLiveHandler(initial Projection) *LiveHandler {
	h := &LiveHandler{}
	h.Set(initial)
	return h
}

func (h *LiveHandler) Set(projection Projection) {
	h.body.Store(projection.body)
}

func (h *LiveHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.body.Load().(string)))
}

type staticHandler struct{ body string }

func (h staticHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.body))
}

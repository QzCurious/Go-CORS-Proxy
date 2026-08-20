package gateway

import (
	"net/http"
	"sync/atomic"
)

// livePACHandler serves the latest PAC content adopted by Gateway.
type livePACHandler struct {
	content atomic.Value
}

func newLivePACHandler(initial string) *livePACHandler {
	h := &livePACHandler{}
	h.Set(initial)
	return h
}

func (h *livePACHandler) Set(content string) {
	h.content.Store(content)
}

func (h *livePACHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.content.Load().(string)))
}

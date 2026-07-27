package pacrouting

import (
	"net/http"
	"sync/atomic"
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

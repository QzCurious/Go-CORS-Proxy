package pacrouting

import (
	"net/http"
	"sync/atomic"
)

type dynamicHandler struct {
	body atomic.Value
}

func newDynamicHandler(body string) *dynamicHandler {
	h := &dynamicHandler{}
	h.Set(body)
	return h
}

func (h *dynamicHandler) Set(body string) {
	h.body.Store(body)
}

func (h *dynamicHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.body.Load().(string)))
}

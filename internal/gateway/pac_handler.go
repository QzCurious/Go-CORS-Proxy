package gateway

import (
	"net/http"
	"sync/atomic"
)

type servedTrafficProjection struct {
	pacContent string
	proxy      http.Handler
}

type liveTrafficProjection struct {
	current atomic.Pointer[servedTrafficProjection]
}

func newLiveTrafficProjection() *liveTrafficProjection { return &liveTrafficProjection{} }

func (l *liveTrafficProjection) Store(next *servedTrafficProjection) { l.current.Store(next) }

func (l *liveTrafficProjection) serveProxy(w http.ResponseWriter, req *http.Request) {
	l.current.Load().proxy.ServeHTTP(w, req)
}

func (l *liveTrafficProjection) servePAC(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(l.current.Load().pacContent))
}

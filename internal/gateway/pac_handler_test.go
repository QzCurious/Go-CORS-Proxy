package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveTrafficProjectionServesPACFromCurrentProjection(t *testing.T) {
	handler := newLiveTrafficProjection()
	handler.Store(&servedTrafficProjection{pacContent: "latest PAC", proxy: http.NotFoundHandler()})

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/seamless-cors.pac", nil)
	response := httptest.NewRecorder()
	handler.servePAC(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "latest PAC" {
		t.Fatalf("handler response = %d %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/x-ns-proxy-autoconfig" {
		t.Fatalf("Content-Type = %q", got)
	}
}

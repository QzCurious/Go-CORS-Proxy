package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLivePACHandlerServesLatestContent(t *testing.T) {
	handler := newLivePACHandler("initial PAC")
	handler.Set("latest PAC")

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/seamless-cors.pac", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "latest PAC" {
		t.Fatalf("handler response = %d %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/x-ns-proxy-autoconfig" {
		t.Fatalf("Content-Type = %q", got)
	}
}

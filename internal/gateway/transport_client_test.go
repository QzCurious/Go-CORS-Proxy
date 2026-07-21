package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStartSendsTypedRequestWithOwnerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/start" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get(tokenHeader); got != "owner-token" {
			t.Fatalf("token header = %q", got)
		}
		var request StartRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.PACReplacementConsent == nil || !request.PACReplacementConsent.Accepted || request.PACReplacementConsent.Fingerprint != "foreign-state-v1" {
			t.Fatalf("request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(StartResult{Kind: StartResultStarted})
	}))
	defer server.Close()

	client := newClient(stateCache{
		HTTPRouterListen: server.Listener.Addr().String(),
		Token:            "owner-token",
	})
	result, err := client.Start(StartRequest{
		PACReplacementConsent: &PACReplacementConsentInput{Accepted: true, Fingerprint: "foreign-state-v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StartResultStarted {
		t.Fatalf("result = %s", result.Kind)
	}
}

func TestStartFailureReturnsCompletedCAEnsure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":500,"detail":"PAC install failed","caEnsure":{"kind":"installed","expires":"0001-01-01T00:00:00Z"}}`))
	}))
	defer server.Close()
	client := newClient(stateCache{HTTPRouterListen: server.Listener.Addr().String()})

	result, err := client.Start(StartRequest{})
	var startErr *StartError
	if !errors.As(err, &startErr) {
		t.Fatalf("error = %v, want StartError", err)
	}
	if result.CAEnsure == nil || result.CAEnsure.Kind != CAEnsureResultInstalled {
		t.Fatalf("CA result = %#v", result.CAEnsure)
	}
}

func TestStatusReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newClient(stateCache{
		HTTPRouterListen: server.Listener.Addr().String(),
		Token:            "bad-token",
	})
	if _, err := client.Status(); err == nil {
		t.Fatal("expected status error")
	}
}

package gateway

import (
	"context"
	"encoding/json"
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
		if request.ManagedPACConsent == nil || request.ManagedPACConsent.Fingerprint != "services-v1" || len(request.ManagedPACConsent.ServiceNames) != 1 || request.ManagedPACConsent.ServiceNames[0] != "Wi-Fi" {
			t.Fatalf("request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(startSuccessBody{Changed: true})
	}))
	defer server.Close()

	client := newClient(stateCache{
		HTTPRouterListen: server.Listener.Addr().String(),
		Token:            "owner-token",
	})
	result, err := client.Start(context.Background(), StartRequest{
		ManagedPACConsent: &ManagedPACConsentInput{ServiceNames: []string{"Wi-Fi"}, Fingerprint: "services-v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StartResultStarted {
		t.Fatalf("result = %s", result.Kind)
	}
}

func TestStartFailureReturnsSemanticResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"managed-pac-installation-failed","message":"Gateway Start was not fulfilled.","details":{"diagnostic":"PAC install failed"}}}`))
	}))
	defer server.Close()
	client := newClient(stateCache{HTTPRouterListen: server.Listener.Addr().String()})

	result, err := client.Start(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StartResultManagedPACInstallationFailed || result.Diagnostic != "PAC install failed" {
		t.Fatalf("result = %#v", result)
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
	if _, err := client.Status(context.Background()); err == nil {
		t.Fatal("expected status error")
	}
}

func TestInstallDecodesApprovalDenialResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"approval-denied","message":"Certificate trust approval was denied."}}`))
	}))
	defer server.Close()
	client := newClient(stateCache{HTTPRouterListen: server.Listener.Addr().String()})

	result, err := client.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != InstallResultApprovalDenied || result.Fulfillment() != CommandUnfulfilled {
		t.Fatalf("install result = %#v", result)
	}
}

func TestCommandClientHasNoHiddenTotalTimeout(t *testing.T) {
	client := newClient(stateCache{})
	if client.httpClient.Timeout != 0 {
		t.Fatalf("command client timeout = %s, want caller-controlled deadline", client.httpClient.Timeout)
	}
}

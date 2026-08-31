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
		if request.WorkingDirectory != "/project" {
			t.Fatalf("request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(startSuccessBody{Changed: true, Guidance: &StartGuidance{
			Traffic:     TrafficStatusDetail{HTTPSCORS: TrafficFeatureBlocked},
			UserCAIssue: &UserCAAssessmentIssue{Cause: "trust store unavailable"},
		}})
	}))
	defer server.Close()

	client := newClient(stateCache{
		HTTPRouterListen: server.Listener.Addr().String(),
		Token:            "owner-token",
	})
	result, err := client.Start(context.Background(), StartRequest{WorkingDirectory: "/project"})
	if err != nil {
		t.Fatal(err)
	}
	started, ok := result.(Started)
	if !ok || started.Guidance.Traffic.HTTPSCORS != TrafficFeatureBlocked ||
		started.Guidance.UserCAIssue == nil || started.Guidance.UserCAIssue.Cause != "trust store unavailable" {
		t.Fatalf("result = %s", result.Kind())
	}
}

func TestStartFailureReturnsSemanticResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"managed-pac-installation-failed","message":"Gateway Start was not fulfilled.","details":{"diagnostic":"PAC install failed","upstreamListCreationWarning":{"cause":"creation denied"}}}}`))
	}))
	defer server.Close()
	client := newClient(stateCache{HTTPRouterListen: server.Listener.Addr().String()})

	result, err := client.Start(context.Background(), StartRequest{WorkingDirectory: "/project"})
	if err != nil {
		t.Fatal(err)
	}
	failed, ok := result.(StartManagedPACInstallationFailed)
	if !ok || failed.Diagnostic != "PAC install failed" || failed.UpstreamListCreationWarningDetail() == nil || failed.UpstreamListCreationWarningDetail().Cause != "creation denied" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCommandClientHasNoHiddenTotalTimeout(t *testing.T) {
	client := newClient(stateCache{})
	if client.httpClient.Timeout != 0 {
		t.Fatalf("command client timeout = %s, want caller-controlled deadline", client.httpClient.Timeout)
	}
}

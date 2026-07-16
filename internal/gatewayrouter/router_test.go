package gatewayrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"seamless-cors/internal/gatewayfacade"
)

func TestDocsAndOpenAPIDoNotRequireToken(t *testing.T) {
	server := New("token", &fakeFacade{})

	for _, path := range []string{"/docs", "/openapi.json"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		server.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d, want %d: %s", path, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
}

func TestCommandRoutesRequireToken(t *testing.T) {
	facade := &fakeFacade{}
	server := New("token", facade)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status returned %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if facade.statusCalled {
		t.Fatal("facade Status was called without token")
	}
}

func TestHealthRequiresTokenAndDoesNotCallFacade(t *testing.T) {
	facade := &fakeFacade{}
	server := New("token", facade)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("health without token returned %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(tokenHeader, "token")
	rec = httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("health returned %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if facade.statusCalled {
		t.Fatal("health should not call facade Status")
	}
}

func TestStartAllowsEmptyBody(t *testing.T) {
	facade := &fakeFacade{}
	server := New("token", facade)

	req := httptest.NewRequest(http.MethodPost, "/start", nil)
	req.Header.Set(tokenHeader, "token")
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("start returned %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !facade.startCalled {
		t.Fatal("facade ExecuteStart was not called")
	}
	if facade.startRequest.PACReplacementConsent != nil {
		t.Fatalf("start request pac replacement consent = %#v, want nil", facade.startRequest.PACReplacementConsent)
	}
}

func TestStartPlanRouteDoesNotExist(t *testing.T) {
	server := New("token", &fakeFacade{})
	req := httptest.NewRequest(http.MethodGet, "/start/plan", nil)
	req.Header.Set(tokenHeader, "token")
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /start/plan returned %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStartUsesOwnerLifecycleContext(t *testing.T) {
	facade := &fakeFacade{}
	server := New("token", facade)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/start", nil)
	req.Header.Set(tokenHeader, "token")
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("start returned %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if facade.startContext == nil {
		t.Fatal("facade ExecuteStart was not called")
	}
	if err := facade.startContext.Err(); err != nil {
		t.Fatalf("start context err = %v, want nil", err)
	}
}

func TestStartFailureCarriesCompletedCAEnsure(t *testing.T) {
	ca := &gatewayfacade.CAEnsureResult{Kind: gatewayfacade.CAEnsureResultInstalled}
	facade := &fakeFacade{startErr: &gatewayfacade.StartError{Diagnostic: "PAC install failed", CAEnsure: ca}}
	server := New("token", facade)
	req := httptest.NewRequest(http.MethodPost, "/start", nil)
	req.Header.Set(tokenHeader, "token")
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("start status = %d, want %d: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var body struct {
		Detail   string                        `json:"detail"`
		CAEnsure *gatewayfacade.CAEnsureResult `json:"caEnsure"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Detail != "PAC install failed" || body.CAEnsure == nil || body.CAEnsure.Kind != gatewayfacade.CAEnsureResultInstalled {
		t.Fatalf("failure body = %#v", body)
	}
}

func TestOpenAPIDocumentsGatewayOwnerToken(t *testing.T) {
	server := New("token", &fakeFacade{})
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("openapi returned %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	components := spec["components"].(map[string]any)
	securitySchemes := components["securitySchemes"].(map[string]any)
	scheme := securitySchemes["gatewayOwnerToken"].(map[string]any)
	if scheme["name"] != tokenHeader {
		t.Fatalf("security scheme name = %v, want %s", scheme["name"], tokenHeader)
	}
	if scheme["in"] != "header" {
		t.Fatalf("security scheme in = %v, want header", scheme["in"])
	}

	paths := spec["paths"].(map[string]any)
	statusPath := paths["/status"].(map[string]any)
	getStatus := statusPath["get"].(map[string]any)
	security := getStatus["security"].([]any)
	if len(security) != 1 {
		t.Fatalf("status security length = %d, want 1", len(security))
	}
	requirement := security[0].(map[string]any)
	if _, ok := requirement["gatewayOwnerToken"]; !ok {
		t.Fatalf("status security = %#v, want gatewayOwnerToken", requirement)
	}
}

type fakeFacade struct {
	startCalled  bool
	statusCalled bool
	startRequest gatewayfacade.StartRequest
	startContext context.Context
	startErr     error
}

func (f *fakeFacade) ExecuteStart(ctx context.Context, request gatewayfacade.StartRequest) (gatewayfacade.StartResult, error) {
	f.startCalled = true
	f.startRequest = request
	f.startContext = ctx
	return gatewayfacade.StartResult{Kind: gatewayfacade.StartResultStarted}, f.startErr
}

func (f *fakeFacade) Stop() (gatewayfacade.StopResult, error) {
	return gatewayfacade.StopResult{Kind: gatewayfacade.StopResultCleanupFailed}, nil
}

func (f *fakeFacade) Status(bool) (gatewayfacade.StatusResult, error) {
	f.statusCalled = true
	return gatewayfacade.StatusResult{Kind: gatewayfacade.GatewayStatusRouterOnly}, nil
}

func (f *fakeFacade) Install() (gatewayfacade.InstallResult, error) {
	return gatewayfacade.InstallResult{Kind: gatewayfacade.InstallResultAlreadyUsable}, nil
}

func (f *fakeFacade) Uninstall() (gatewayfacade.UninstallResult, error) {
	return gatewayfacade.UninstallResult{Kind: gatewayfacade.UninstallResultAlreadyAbsent}, nil
}

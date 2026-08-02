package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDocsAndOpenAPIDoNotRequireToken(t *testing.T) {
	server := newRouter("token", &fakeCommandHandler{})

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
	handler := &fakeCommandHandler{}
	server := newRouter("token", handler)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status returned %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if handler.statusCalled {
		t.Fatal("command handler Status was called without token")
	}
	var body struct {
		Error gatewayErrorBody `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != errorCodeUnauthorized || body.Error.Message == "" {
		t.Fatalf("unauthorized body = %#v", body)
	}
}

func TestHealthRequiresTokenAndDoesNotCallCommandHandler(t *testing.T) {
	handler := &fakeCommandHandler{}
	server := newRouter("token", handler)

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
	if handler.statusCalled {
		t.Fatal("health should not call command handler Status")
	}
}

func TestStartAllowsEmptyBody(t *testing.T) {
	handler := &fakeCommandHandler{}
	server := newRouter("token", handler)

	req := httptest.NewRequest(http.MethodPost, "/start", nil)
	req.Header.Set(tokenHeader, "token")
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("start returned %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !handler.startCalled {
		t.Fatal("command handler ExecuteStart was not called")
	}
	if handler.startRequest.ManagedPACConsent != nil {
		t.Fatalf("start request Managed PAC Consent = %#v, want nil", handler.startRequest.ManagedPACConsent)
	}
}

func TestStartPlanRouteDoesNotExist(t *testing.T) {
	server := newRouter("token", &fakeCommandHandler{})
	req := httptest.NewRequest(http.MethodGet, "/start/plan", nil)
	req.Header.Set(tokenHeader, "token")
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /start/plan returned %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStartPropagatesRequestContext(t *testing.T) {
	handler := &fakeCommandHandler{}
	server := newRouter("token", handler)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/start", nil)
	req.Header.Set(tokenHeader, "token")
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("start returned %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if handler.startContext == nil {
		t.Fatal("command handler ExecuteStart was not called")
	}
	if err := handler.startContext.Err(); err != context.Canceled {
		t.Fatalf("start context err = %v, want %v", err, context.Canceled)
	}
}

func TestStartFailureUsesSharedErrorShell(t *testing.T) {
	handler := &fakeCommandHandler{startResult: StartManagedPACInstallationFailed{Diagnostic: "PAC install failed"}}
	server := newRouter("token", handler)
	req := httptest.NewRequest(http.MethodPost, "/start", nil)
	req.Header.Set(tokenHeader, "token")
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("start status = %d, want %d: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var body struct {
		Error gatewayErrorBody `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != string(StartResultManagedPACInstallationFailed) {
		t.Fatalf("failure body = %#v", body)
	}
	var details startFailureDetails
	if err := json.Unmarshal(body.Error.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details.Diagnostic != "PAC install failed" {
		t.Fatalf("failure details = %#v", details)
	}
}

func TestStartSuccessIsBareSubjectResponse(t *testing.T) {
	handler := &fakeCommandHandler{startResult: Started{}}
	server := newRouter("token", handler)
	req := httptest.NewRequest(http.MethodPost, "/start", nil)
	req.Header.Set(tokenHeader, "token")
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["changed"] != true {
		t.Fatalf("start body = %#v", body)
	}
	if _, exists := body["kind"]; exists {
		t.Fatalf("start body echoed result kind: %#v", body)
	}
	if _, exists := body["result"]; exists {
		t.Fatalf("start body used a success envelope: %#v", body)
	}
}

func TestOpenAPIDocumentsGatewayOwnerToken(t *testing.T) {
	server := newRouter("token", &fakeCommandHandler{})
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

type fakeCommandHandler struct {
	startCalled  bool
	statusCalled bool
	startRequest StartRequest
	startContext context.Context
	startErr     error
	startResult  StartResult
}

func (f *fakeCommandHandler) ExecuteStart(ctx context.Context, request StartRequest) (StartResult, error) {
	f.startCalled = true
	f.startRequest = request
	f.startContext = ctx
	result := f.startResult
	if result == nil {
		result = Started{}
	}
	return result, f.startErr
}

func (f *fakeCommandHandler) Stop(context.Context) (StopResult, error) {
	return StopResult{Kind: StopResultCleanupFailed}, nil
}

func (f *fakeCommandHandler) Status(context.Context, bool) (StatusResult, error) {
	f.statusCalled = true
	return StatusResult{Kind: StatusResultReported, StatusReport: StatusReport{State: GatewayStatusRouterOnly}}, nil
}

func (f *fakeCommandHandler) Install(context.Context) (InstallResult, error) {
	return InstallResult{Kind: InstallResultAlreadyUsable}, nil
}

func (f *fakeCommandHandler) UninstallWithConsent(context.Context, string) (UninstallResult, error) {
	return UninstallResult{Kind: UninstallResultAlreadyAbsent}, nil
}

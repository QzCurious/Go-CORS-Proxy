package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/corsproxy"
	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

func TestRuntimeClassifiesInitialFileSyncIssue(t *testing.T) {
	observed := &fileobservation.ReadError{Path: "/tmp/upstreams.txt", Cause: errors.New("source unavailable")}
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Result{Err: observed})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	issue := runtime.snapshot().UpstreamListFileSyncIssue
	if issue == nil || issue.Kind != FileSyncIssueFileUnreadable || !strings.Contains(issue.Cause, "source unavailable") {
		t.Fatalf("file sync issue = %#v", issue)
	}
}

func TestProjectionFailureIsIndependentAndFailClosed(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Result{Contents: []byte("api.example.test\n")})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	if err := runtime.applyUpstreamListResult(fileobservation.Result{Contents: []byte{0xff}}); err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.UpstreamListProjectionIssue == nil || state.UpstreamListFileSyncIssue != nil || state.UpstreamCount != 0 {
		t.Fatalf("rejected contents state = %#v", state)
	}

	readErr := &fileobservation.ReadError{Path: "/tmp/upstreams.txt", Cause: errors.New("temporarily unavailable")}
	if err := runtime.applyUpstreamListResult(fileobservation.Result{Err: readErr}); err != nil {
		t.Fatal(err)
	}
	state = runtime.snapshot()
	if state.UpstreamListFileSyncIssue == nil || state.UpstreamListProjectionIssue == nil || state.UpstreamCount != 0 {
		t.Fatalf("observation failure did not preserve projection state = %#v", state)
	}
}

func TestSuccessfulEqualProjectionClearsIssuesWithoutPACPublication(t *testing.T) {
	readErr := &fileobservation.ReadError{Path: "/tmp/upstreams.txt", Cause: errors.New("temporarily unavailable")}
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Result{Err: readErr})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	if err := runtime.applyUpstreamListResult(fileobservation.Result{Contents: []byte{0xff}}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.applyUpstreamListResult(fileobservation.Result{Contents: nil}); err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.UpstreamListFileSyncIssue != nil || state.UpstreamListProjectionIssue != nil || state.UpstreamCount != 0 {
		t.Fatalf("cleared equal projection state = %#v", state)
	}
	select {
	case projection := <-runtime.PACProjections():
		t.Fatalf("equal empty projection published PAC projection: %#v", projection)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestEquivalentFileSyncIssueDoesNotInvalidateStatusAgain(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Result{Contents: nil})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	result := fileobservation.Result{Err: &fileobservation.ReadError{Path: "/tmp/upstreams.txt", Cause: errors.New("missing")}}

	if err := runtime.applyUpstreamListResult(result); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.RuntimeChanges():
	case <-time.After(time.Second):
		t.Fatal("first issue did not invalidate status")
	}
	if err := runtime.applyUpstreamListResult(result); err != nil {
		t.Fatal(err)
	}
	select {
	case kind := <-runtime.RuntimeChanges():
		t.Fatalf("equivalent issue invalidated status: %v", kind)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPACPublicationInputIgnoresRepresentationAndWarningOnlyChanges(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchUpstreamList(ctx, errs)
	desired := runtime.PACProjections()

	writeTrafficTestFile(t, upstreamPath, "API.EXAMPLE.TEST\nhttps://bad.example.test/path\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return len(state.UpstreamListWarnings) == 1
	})
	select {
	case state := <-desired:
		t.Fatalf("warning-only source state published desired PAC input: %#v", state)
	case <-time.After(250 * time.Millisecond):
	}

	writeTrafficTestFile(t, upstreamPath, "changed.example.test\n")
	select {
	case state := <-desired:
		if !strings.Contains(state.Body(), "changed.example.test") {
			t.Fatalf("PAC projection does not contain changed host: %s", state.Body())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for desired Upstream List input")
	}
}

func TestPACPublicationInputIgnoresInactiveHTTPSRouteChanges(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(context.Background(), userca.Assessment{}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.PACProjections():
	default:
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchUpstreamList(ctx, errs)
	desired := runtime.PACProjections()

	writeTrafficTestFile(t, upstreamPath, "api.example.test\nhttps://secure.example.test\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool { return state.HTTPSIntent })
	select {
	case state := <-desired:
		t.Fatalf("inactive HTTPS selector published PAC input: %#v", state)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestHTTPSIntentDoesNotReassessLatchedUserCA(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(context.Background(), userca.Assessment{}, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchUpstreamList(ctx, errs)

	writeTrafficTestFile(t, upstreamPath, "api.example.test\nhttps://secure.example.test\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool { return state.HTTPSIntent })

	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessNotReady || !hasHTTPSWarning(state.HTTPSWarnings, HTTPSWarningUnmetIntent) {
		t.Fatalf("latched UserCA state = %#v", state)
	}
}

func TestRecoverHTTPSPublishesCompleteDesiredPACInput(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(context.Background(), userca.Assessment{}, nil); err != nil {
		t.Fatal(err)
	}
	err = runtime.RecoverHTTPS(context.Background(), testUserCASnapshot(t, time.Now().Add(24*time.Hour), false))

	if err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessReady || state.HTTPSInterception != HTTPSInterceptionActive {
		t.Fatalf("recovered state = %#v", state)
	}
	select {
	case desired := <-runtime.PACProjections():
		if !strings.Contains(desired.Body(), "secure.example.test") {
			t.Fatalf("PAC projection did not enable HTTPS route: %s", desired.Body())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for desired PAC state")
	}
}

func TestInitialCertificateProjectionFailureStartsHTTPSDegraded(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Result{Contents: []byte("https://secure.example.test\n")})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	expiresAt := time.Now().Add(24 * time.Hour)
	snapshot, err := userca.NewSnapshot(expiresAt, false)
	if err != nil {
		t.Fatal(err)
	}
	assessment := userca.NewAssessment(snapshot, testProviderSource{
		validUntil: expiresAt,
		project: func(context.Context, upstreamlist.Projection) (userca.CertificateProvider, error) {
			return nil, errors.New("key generation unavailable")
		},
	})

	if err := runtime.SetInitialHTTPSReadiness(context.Background(), assessment, nil); err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessReady || state.HTTPSInterception != HTTPSInterceptionFailed {
		t.Fatalf("initial degraded state = %#v", state)
	}
	if !hasHTTPSWarning(state.HTTPSWarnings, HTTPSWarningInterceptionFailed) ||
		strings.Contains(runtime.currentPACProjection().Body(), "secure.example.test") {
		t.Fatalf("initial degraded projection = %#v PAC=%s", state, runtime.currentPACProjection().Body())
	}
}

func TestSuccessfulSameListObservationRetriesFailedCertificateProjection(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Result{Contents: []byte("https://first.example.test\n")})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	expiresAt := time.Now().Add(24 * time.Hour)
	provider := testUserCAProvider{validUntil: expiresAt}
	var projectionErr error
	source := testProviderSource{
		validUntil: expiresAt,
		project: func(context.Context, upstreamlist.Projection) (userca.CertificateProvider, error) {
			if projectionErr != nil {
				return nil, projectionErr
			}
			return provider, nil
		},
	}
	snapshot, err := userca.NewSnapshot(expiresAt, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetInitialHTTPSReadiness(context.Background(), userca.NewAssessment(snapshot, source), nil); err != nil {
		t.Fatal(err)
	}

	projectionErr = errors.New("random unavailable")
	update := fileobservation.Result{Contents: []byte("https://second.example.test\n")}
	if err := runtime.applyUpstreamListResult(update); err != nil {
		t.Fatal(err)
	}
	if failed := runtime.snapshot(); failed.HTTPSInterception != HTTPSInterceptionFailed || failed.UpstreamCount != 1 {
		t.Fatalf("failed list adoption = %#v", failed)
	}
	projectionErr = nil
	if err := runtime.applyUpstreamListResult(update); err != nil {
		t.Fatal(err)
	}
	if recovered := runtime.snapshot(); recovered.HTTPSInterception != HTTPSInterceptionActive {
		t.Fatalf("same-list retry state = %#v", recovered)
	}
	if body := runtime.currentPACProjection().Body(); !strings.Contains(body, "second.example.test") || strings.Contains(body, "first.example.test") {
		t.Fatalf("recovered PAC = %s", body)
	}
}

func TestInterceptionFailurePreservesLatchedUserCAAndInstallCanRecover(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	snapshot := testUserCASnapshot(t, time.Now().Add(24*time.Hour), false)
	if err := runtime.SetInitialHTTPSReadiness(context.Background(), snapshot, nil); err != nil {
		t.Fatal(err)
	}

	runtime.handleHTTPSFailure(corsproxy.HTTPSFailure{
		Disposition: corsproxy.HTTPSFailureProvider,
		Err:         context.DeadlineExceeded,
	})
	failed := runtime.snapshot()
	if failed.HTTPSReadiness != HTTPSReadinessReady || failed.HTTPSInterception != HTTPSInterceptionFailed {
		t.Fatalf("failed state = %#v", failed)
	}
	if !hasHTTPSWarning(failed.HTTPSWarnings, HTTPSWarningInterceptionFailed) {
		t.Fatalf("failure warnings = %#v", failed.HTTPSWarnings)
	}

	if err := runtime.RecoverHTTPS(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if recovered := runtime.snapshot(); recovered.HTTPSInterception != HTTPSInterceptionActive {
		t.Fatalf("recovered state = %#v", recovered)
	}
}

func TestUserCAExpiryWithdrawsHTTPSAndDirectsExplicitInstall(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(context.Background(), testUserCASnapshot(t, time.Now().Add(time.Hour), false), nil); err != nil {
		t.Fatal(err)
	}

	runtime.handleHTTPSFailure(corsproxy.HTTPSFailure{
		Disposition: corsproxy.HTTPSFailureExpired,
		Err:         context.DeadlineExceeded,
	})

	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessReady || state.HTTPSInterception != HTTPSInterceptionActive {
		t.Fatalf("expiry signal changed state before Gateway assessment = %#v", state)
	}
	select {
	case kind := <-runtime.RuntimeChanges():
		if kind != HTTPSDeadlineReached {
			t.Fatalf("expiry signal kind = %v", kind)
		}
	case <-time.After(time.Second):
		t.Fatal("expiry signal was not published")
	}
}

func TestHTTPSDeadlineSignalSurvivesStatusInvalidation(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	runtime.publishRuntimeChange(HTTPSDeadlineReached)
	runtime.publishRuntimeChange(RuntimeStatusChanged)

	select {
	case got := <-runtime.RuntimeChanges():
		if got != HTTPSDeadlineReached {
			t.Fatalf("runtime change = %v, want deadline signal", got)
		}
	case <-time.After(time.Second):
		t.Fatal("deadline signal was lost")
	}
}

func TestRuntimeStatusDerivesEffectiveExpiryFromLatchedSnapshot(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	expiresAt := time.Now().Add(time.Hour)
	if err := runtime.SetInitialHTTPSReadiness(context.Background(), testUserCASnapshot(t, expiresAt, false), nil); err != nil {
		t.Fatal(err)
	}
	runtime.now = func() time.Time { return expiresAt.Add(time.Second) }

	state := runtime.snapshot()

	if state.HTTPSReadiness != HTTPSReadinessNotReady || state.HTTPSInterception != HTTPSInterceptionInactive {
		t.Fatalf("effective expiry state = %#v", state)
	}
	if !strings.Contains(httpsWarningDiagnostics(state.HTTPSWarnings), "install") {
		t.Fatalf("effective expiry warnings = %#v", state.HTTPSWarnings)
	}
}

func TestHTTPSReadinessWarningsUseOnlySemanticUserCAState(t *testing.T) {
	noIntent := upstreamListForTrafficTest(t, "api.example.test\n")
	intent := upstreamListForTrafficTest(t, "https://api.example.test\n")
	expiry := time.Date(2030, time.January, 2, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		list     upstreamlist.Projection
		snapshot userca.Snapshot
		err      error
		want     string
	}{
		{name: "not usable without intent is silent", list: noIntent},
		{name: "not usable with intent asks for install", list: intent, want: "not usable"},
		{
			name:     "renewal due stays ready",
			list:     noIntent,
			snapshot: testUserCASnapshot(t, expiry, true).Snapshot(),
			want:     "expires soon",
		},
		{
			name: "assessment failure is distinct from state",
			list: noIntent,
			err:  context.DeadlineExceeded,
			want: "could not be assessed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := httpsWarningDiagnostics(httpsReadinessWarnings(test.list.HTTPSIntent(), test.snapshot, test.err))
			if !strings.Contains(got, test.want) {
				t.Fatalf("warning = %q, want substring %q", got, test.want)
			}
		})
	}
}

func testUserCASnapshot(t *testing.T, expiresAt time.Time, renewalDue bool) userca.Assessment {
	t.Helper()
	snapshot, err := userca.NewSnapshot(expiresAt, renewalDue)
	if err != nil {
		t.Fatal(err)
	}
	return userca.NewAssessment(snapshot, testUserCAProvider{validUntil: expiresAt})
}

type testUserCAProvider struct{ validUntil time.Time }

func (p testUserCAProvider) Project(context.Context, upstreamlist.Projection) (userca.CertificateProvider, error) {
	return p, nil
}

func (p testUserCAProvider) CertificateFor(string) (*tls.Certificate, error) {
	return &tls.Certificate{}, nil
}

func (p testUserCAProvider) ValidUntil() time.Time { return p.validUntil }

type testProviderSource struct {
	validUntil time.Time
	project    func(context.Context, upstreamlist.Projection) (userca.CertificateProvider, error)
}

func (s testProviderSource) Project(ctx context.Context, projection upstreamlist.Projection) (userca.CertificateProvider, error) {
	return s.project(ctx, projection)
}

func (s testProviderSource) ValidUntil() time.Time { return s.validUntil }

func httpsWarningDiagnostics(warnings []HTTPSWarningDetail) string {
	var diagnostics []string
	for _, warning := range warnings {
		diagnostics = append(diagnostics, warning.Diagnostic+" "+warning.Action)
	}
	return strings.Join(diagnostics, "\n")
}

func upstreamListForTrafficTest(t *testing.T, contents string) upstreamlist.Projection {
	t.Helper()
	projection, err := upstreamlist.Project([]byte(contents))
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func closeTrafficTestRuntime(runtime *trafficRuntime) {
	for _, listener := range runtime.listeners {
		_ = listener.Close()
	}
}

func writeTrafficTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func createTrafficConfig(t *testing.T, upstreams string) (*fileobservation.Observation, fileobservation.Result, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return createTrafficConfigAtCurrentHome(t, upstreams)
}

func createTrafficConfigAtCurrentHome(t *testing.T, upstreams string) (*fileobservation.Observation, fileobservation.Result, string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".seamless-cors")
	upstreamPath := filepath.Join(dir, "upstreams.txt")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTrafficTestFile(t, upstreamPath, upstreams)
	source, err := fileobservation.Open(upstreamPath, fileobservation.Options{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case initial := <-source.Results():
		return source, initial, upstreamPath
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial Upstream List state")
		return nil, fileobservation.Result{}, ""
	}
}

func waitForTrafficConfig(t *testing.T, runtime *trafficRuntime, errs <-chan serverError, ready func(runtimeState) bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready(runtime.snapshot()) {
			return
		}
		select {
		case err := <-errs:
			t.Fatal(err.err)
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("timed out waiting for Upstream List update")
		}
	}
}

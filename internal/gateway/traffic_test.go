package gateway

import (
	"context"
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/corsproxy"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

func TestRuntimeRetainsObservationError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-upstreams.txt")
	runtime, err := newRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	diagnostics := runtime.snapshot().UpstreamListDiagnostics
	if diagnostics == nil || diagnostics.ObservationError == nil || diagnostics.ObservationError.Kind != UpstreamListObservationReadFailed {
		t.Fatalf("source diagnostics = %#v", diagnostics)
	}
}

func TestRuntimeTracksFormatAndObservationDiagnosticsIndependently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := newRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	diagnostics := runtime.snapshot().UpstreamListDiagnostics
	if diagnostics == nil || diagnostics.InvalidFormat == nil || diagnostics.ObservationError != nil {
		t.Fatalf("initial diagnostics = %#v", diagnostics)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	waitForTrafficConfig(t, runtime, func(state runtimeState) bool {
		return state.UpstreamListDiagnostics != nil && state.UpstreamListDiagnostics.InvalidFormat != nil && state.UpstreamListDiagnostics.ObservationError != nil
	})

	writeTrafficTestFile(t, path, "api.example.test\n")
	waitForTrafficConfig(t, runtime, func(state runtimeState) bool {
		return state.UpstreamListDiagnostics == nil && state.UpstreamCount == 1
	})
}

func TestPACPublicationInputIgnoresRepresentationAndWarningOnlyChanges(t *testing.T) {
	_, _, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	desired := runtime.DesiredStates()

	writeTrafficTestFile(t, upstreamPath, "API.EXAMPLE.TEST\nhttps://bad.example.test/path\n")
	waitForTrafficConfig(t, runtime, func(state runtimeState) bool {
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
		entries := state.UpstreamList.HostSelectors
		if len(entries) != 1 || entries[0].Hostname != "changed.example.test" {
			t.Fatalf("desired entries = %#v", entries)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for desired Upstream List input")
	}
}

func TestPACPublicationInputIgnoresInactiveHTTPSRouteChanges(t *testing.T) {
	_, _, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(userca.Assessment{}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.DesiredStates():
	default:
	}

	desired := runtime.DesiredStates()

	writeTrafficTestFile(t, upstreamPath, "api.example.test\nhttps://secure.example.test\n")
	waitForTrafficConfig(t, runtime, func(state runtimeState) bool { return state.HTTPSIntent })
	select {
	case state := <-desired:
		t.Fatalf("inactive HTTPS selector published PAC input: %#v", state)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestHTTPSIntentDoesNotReassessLatchedUserCA(t *testing.T) {
	_, _, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(userca.Assessment{}, nil); err != nil {
		t.Fatal(err)
	}
	writeTrafficTestFile(t, upstreamPath, "api.example.test\nhttps://secure.example.test\n")
	waitForTrafficConfig(t, runtime, func(state runtimeState) bool { return state.HTTPSIntent })

	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessNotReady || !hasHTTPSWarning(state.HTTPSWarnings, HTTPSWarningUnmetIntent) {
		t.Fatalf("latched UserCA state = %#v", state)
	}
}

func TestRecoverHTTPSPublishesCompleteDesiredPACInput(t *testing.T) {
	_, _, upstreamPath := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(userca.Assessment{}, nil); err != nil {
		t.Fatal(err)
	}
	err = runtime.RecoverHTTPS(testUserCASnapshot(t, time.Now().Add(24*time.Hour), false))

	if err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessReady || state.HTTPSInterception != HTTPSInterceptionActive {
		t.Fatalf("recovered state = %#v", state)
	}
	select {
	case desired := <-runtime.DesiredStates():
		if !desired.HTTPSInterception {
			t.Fatalf("desired state did not enable HTTPS interception: %#v", desired)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for desired PAC state")
	}
}

func TestInterceptionFailurePreservesLatchedUserCAAndInstallCanRecover(t *testing.T) {
	_, _, upstreamPath := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	snapshot := testUserCASnapshot(t, time.Now().Add(24*time.Hour), false)
	if err := runtime.SetInitialHTTPSReadiness(snapshot, nil); err != nil {
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

	if err := runtime.RecoverHTTPS(snapshot); err != nil {
		t.Fatal(err)
	}
	if recovered := runtime.snapshot(); recovered.HTTPSInterception != HTTPSInterceptionActive {
		t.Fatalf("recovered state = %#v", recovered)
	}
}

func TestUserCAExpiryWithdrawsHTTPSAndDirectsExplicitInstall(t *testing.T) {
	_, _, upstreamPath := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(testUserCASnapshot(t, time.Now().Add(time.Hour), false), nil); err != nil {
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
	_, _, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath)
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
	_, _, upstreamPath := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	expiresAt := time.Now().Add(time.Hour)
	if err := runtime.SetInitialHTTPSReadiness(testUserCASnapshot(t, expiresAt, false), nil); err != nil {
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
		list     upstreamlist.UpstreamList
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

func (p testUserCAProvider) CertificateFor(string) (*tls.Certificate, error) {
	return &tls.Certificate{}, nil
}

func (p testUserCAProvider) ValidUntil() time.Time { return p.validUntil }

func httpsWarningDiagnostics(warnings []HTTPSWarningDetail) string {
	var diagnostics []string
	for _, warning := range warnings {
		diagnostics = append(diagnostics, warning.Diagnostic+" "+warning.Action)
	}
	return strings.Join(diagnostics, "\n")
}

func upstreamListForTrafficTest(t *testing.T, contents string) upstreamlist.UpstreamList {
	t.Helper()
	_, snapshot, _ := createTrafficConfig(t, contents)
	return snapshot.(upstreamlist.Projection).List
}

func closeTrafficTestRuntime(runtime *trafficRuntime) {
	_ = runtime.Close()
}

func writeTrafficTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func createTrafficConfig(t *testing.T, upstreams string) (*upstreamlist.Source, upstreamlist.Transition, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return createTrafficConfigAtCurrentHome(t, upstreams)
}

func createTrafficConfigAtCurrentHome(t *testing.T, upstreams string) (*upstreamlist.Source, upstreamlist.Transition, string) {
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
	source := upstreamlist.Open(upstreamPath)
	select {
	case initial := <-source.Transitions():
		_ = source.Close()
		return source, initial, upstreamPath
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial Upstream List state")
		return nil, nil, ""
	}
}

func waitForTrafficConfig(t *testing.T, runtime *trafficRuntime, ready func(runtimeState) bool) {
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
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("timed out waiting for Upstream List update")
		}
	}
}

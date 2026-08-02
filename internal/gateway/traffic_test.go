package gateway

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/corsproxy"
	"github.com/QzCurious/seamless-cors/internal/liveconfig"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

func TestPACVersionFollowsUpstreamListEntriesRevision(t *testing.T) {
	config, initial, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(config, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchLiveConfig(ctx, errs)
	initialPACURL := runtime.PACURL()

	writeTrafficTestFile(t, upstreamPath, "API.EXAMPLE.TEST\nhttps://bad.example.test/path\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return len(state.UpstreamListWarnings) == 1
	})
	if runtime.PACURL() != initialPACURL {
		t.Fatalf("warning-only change advanced PAC URL from %q to %q", initialPACURL, runtime.PACURL())
	}

	writeTrafficTestFile(t, upstreamPath, "changed.example.test\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return state.UpstreamCount == 1 && runtime.PACURL() != initialPACURL
	})
}

func TestHTTPSIntentDoesNotReassessLatchedUserCA(t *testing.T) {
	config, initial, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(config, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(userca.Snapshot{}, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchLiveConfig(ctx, errs)

	writeTrafficTestFile(t, upstreamPath, "api.example.test\nhttps://secure.example.test\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool { return state.HTTPSIntent })

	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessNotReady || !hasHTTPSWarning(state.HTTPSWarnings, HTTPSWarningUnmetIntent) {
		t.Fatalf("latched UserCA state = %#v", state)
	}
}

func TestRecoverHTTPSPublishesNewGenerationAndPAC(t *testing.T) {
	config, initial, _ := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(config, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(userca.Snapshot{}, nil); err != nil {
		t.Fatal(err)
	}
	initialPACURL := runtime.PACURL()

	err = runtime.RecoverHTTPS(testUserCASnapshot(t, time.Now().Add(24*time.Hour), false))

	if err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessReady || state.HTTPSInterception != HTTPSInterceptionActive {
		t.Fatalf("recovered state = %#v", state)
	}
	if runtime.PACURL() == "" || runtime.PACURL() == initialPACURL {
		t.Fatalf("PAC recovery URL = %q, initial = %q", runtime.PACURL(), initialPACURL)
	}
}

func TestInterceptionFailurePreservesLatchedUserCAAndInstallCanRecover(t *testing.T) {
	config, initial, _ := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(config, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	snapshot := testUserCASnapshot(t, time.Now().Add(24*time.Hour), false)
	if err := runtime.SetInitialHTTPSReadiness(snapshot, nil); err != nil {
		t.Fatal(err)
	}

	runtime.handleHTTPSFailure(corsproxy.HTTPSFailure{
		Kind: corsproxy.HTTPSFailureInterception,
		Err:  context.DeadlineExceeded,
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
	config, initial, _ := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(config, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(testUserCASnapshot(t, time.Now().Add(time.Hour), false), nil); err != nil {
		t.Fatal(err)
	}

	runtime.handleHTTPSFailure(corsproxy.HTTPSFailure{
		Kind: corsproxy.HTTPSFailureReadiness,
		Err:  context.DeadlineExceeded,
	})

	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessNotReady || state.HTTPSInterception != HTTPSInterceptionInactive {
		t.Fatalf("expiry state = %#v", state)
	}
	if !strings.Contains(httpsWarningDiagnostics(state.HTTPSWarnings), "install") {
		t.Fatalf("expiry warnings = %#v", state.HTTPSWarnings)
	}
}

func TestRuntimeStatusDerivesEffectiveExpiryFromLatchedSnapshot(t *testing.T) {
	config, initial, _ := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(config, initial)
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
		list     liveconfig.Snapshot
		snapshot userca.Snapshot
		err      error
		want     string
	}{
		{name: "not usable without intent is silent", list: noIntent},
		{name: "not usable with intent asks for install", list: intent, want: "not usable"},
		{
			name:     "renewal due stays ready",
			list:     noIntent,
			snapshot: testUserCASnapshot(t, expiry, true),
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
			got := httpsWarningDiagnostics(httpsReadinessWarnings(test.list.UpstreamList().HTTPSIntent(), test.snapshot, test.err))
			if !strings.Contains(got, test.want) {
				t.Fatalf("warning = %q, want substring %q", got, test.want)
			}
		})
	}
}

func testUserCASnapshot(t *testing.T, expiresAt time.Time, renewalDue bool) userca.Snapshot {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: "gateway test UserCA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              expiresAt,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := userca.NewSnapshot(
		tls.Certificate{
			Certificate: [][]byte{der},
			PrivateKey:  key,
			Leaf:        template,
		},
		expiresAt,
		renewalDue,
	)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func httpsWarningDiagnostics(warnings []HTTPSWarningDetail) string {
	var diagnostics []string
	for _, warning := range warnings {
		diagnostics = append(diagnostics, warning.Diagnostic+" "+warning.Action)
	}
	return strings.Join(diagnostics, "\n")
}

func upstreamListForTrafficTest(t *testing.T, contents string) liveconfig.Snapshot {
	t.Helper()
	_, snapshot, _ := createTrafficConfig(t, contents)
	return snapshot
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

func createTrafficConfig(t *testing.T, upstreams string) (*liveconfig.Config, liveconfig.Snapshot, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return createTrafficConfigAtCurrentHome(t, upstreams)
}

func createTrafficConfigAtCurrentHome(t *testing.T, upstreams string) (*liveconfig.Config, liveconfig.Snapshot, string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	config, err := liveconfig.Create()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".seamless-cors")
	upstreamPath := filepath.Join(dir, "upstreams.txt")
	writeTrafficTestFile(t, upstreamPath, upstreams)
	snapshot, err := config.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return config, snapshot, upstreamPath
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
			t.Fatal("timed out waiting for Live Configuration update")
		}
	}
}

package gateway

import (
	"context"
	"crypto/x509"
	"encoding/pem"
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
	defer func() {
		for _, listener := range runtime.listeners {
			_ = listener.Close()
		}
	}()

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
	if runtime.PACURL() == initialPACURL {
		t.Fatalf("Upstream List Entries Revision change did not advance PAC URL %q", initialPACURL)
	}
}

func TestHTTPSIntentDoesNotReassessLatchedReadiness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, initial, upstreamPath := createTrafficConfigAtCurrentHome(t, "api.example.test\n")
	runtime, err := newRuntime(config, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(nil, userca.Report{Health: userca.HealthMissing}, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchLiveConfig(ctx, errs)

	writeTrafficTestFile(t, upstreamPath, "api.example.test\nhttps://secure.example.test\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return state.HTTPSIntent
	})
	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessNotReady {
		t.Fatalf("latched readiness state = %#v", state)
	}
	if !hasHTTPSWarning(state.HTTPSWarnings, HTTPSWarningUnmetIntent) {
		t.Fatalf("HTTPS warnings = %#v", state.HTTPSWarnings)
	}
}

func TestRecoverHTTPSActivatesLatchedReadiness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, initial, _ := createTrafficConfigAtCurrentHome(t, "api.example.test\nhttps://secure.example.test\n")
	runtime, err := newRuntime(config, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetInitialHTTPSReadiness(nil, userca.Report{Health: userca.HealthMissing}, nil); err != nil {
		t.Fatal(err)
	}
	store := &trafficTestTrustStore{}
	authority, result, err := userca.Ensure(filepath.Join(home, "ca"), store)
	if err != nil {
		t.Fatal(err)
	}
	initialPACURL := runtime.PACURL()
	nextPACURL, err := runtime.RecoverHTTPS(authority, result.Report)
	if err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessReady {
		t.Fatalf("recovered readiness state = %#v", state)
	}
	if len(state.HTTPSWarnings) != 0 {
		t.Fatalf("HTTPS warnings = %#v", state.HTTPSWarnings)
	}
	if nextPACURL == "" || nextPACURL == initialPACURL || runtime.PACURL() != nextPACURL {
		t.Fatalf("PAC recovery URL = %q, initial = %q, current = %q", nextPACURL, initialPACURL, runtime.PACURL())
	}
}

func TestInterceptionFailurePreservesReadinessAndInstallRecovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, initial, _ := createTrafficConfigAtCurrentHome(t, "https://secure.example.test\n")
	runtime, err := newRuntime(config, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	store := &trafficTestTrustStore{}
	authority, result, err := userca.Ensure(filepath.Join(home, "ca"), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetInitialHTTPSReadiness(authority, result.Report, nil); err != nil {
		t.Fatal(err)
	}
	initialPAC := runtime.PACURL()

	runtime.handleHTTPSFailure(corsproxy.HTTPSFailure{
		Kind: corsproxy.HTTPSFailureInterception,
		Err:  context.DeadlineExceeded,
	})
	failed := runtime.snapshot()
	if failed.HTTPSReadiness != HTTPSReadinessReady || failed.HTTPSInterception != HTTPSInterceptionFailed {
		t.Fatalf("failed state = %#v", failed)
	}
	if runtime.PACURL() == initialPAC || !hasHTTPSWarning(failed.HTTPSWarnings, HTTPSWarningInterceptionFailed) {
		t.Fatalf("failure did not withdraw HTTPS routes: PAC %q warnings %#v", runtime.PACURL(), failed.HTTPSWarnings)
	}

	if _, err := runtime.RecoverHTTPS(authority, result.Report); err != nil {
		t.Fatal(err)
	}
	recovered := runtime.snapshot()
	if recovered.HTTPSInterception != HTTPSInterceptionActive || len(recovered.HTTPSWarnings) != 0 {
		t.Fatalf("recovered state = %#v", recovered)
	}
}

func TestReadinessLossWithdrawsHTTPSRoutesAndDirectsRecoveryToInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, initial, _ := createTrafficConfigAtCurrentHome(t, "https://secure.example.test\n")
	runtime, err := newRuntime(config, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	store := &trafficTestTrustStore{}
	authority, result, err := userca.Ensure(filepath.Join(home, "ca"), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetInitialHTTPSReadiness(authority, result.Report, nil); err != nil {
		t.Fatal(err)
	}

	runtime.handleHTTPSFailure(corsproxy.HTTPSFailure{
		Kind: corsproxy.HTTPSFailureReadiness,
		Err:  context.DeadlineExceeded,
	})
	state := runtime.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessNotReady || state.HTTPSInterception != HTTPSInterceptionInactive {
		t.Fatalf("readiness-loss state = %#v", state)
	}
	if !strings.Contains(httpsWarningDiagnostics(state.HTTPSWarnings), "expired") {
		t.Fatalf("readiness-loss warnings = %#v", state.HTTPSWarnings)
	}
}

func TestHTTPSReadinessWarningMatrix(t *testing.T) {
	noIntent := upstreamListForTrafficTest(t, "api.example.test\n")
	intent := upstreamListForTrafficTest(t, "https://api.example.test\n")
	expiry := time.Date(2030, time.January, 2, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		list      liveconfig.Snapshot
		authority *userca.Authority
		report    userca.Report
		err       error
		want      string
	}{
		{
			name:   "absent without intent is silent",
			list:   noIntent,
			report: userca.Report{Health: userca.HealthMissing},
		},
		{
			name:   "absent with intent is unmet",
			list:   intent,
			report: userca.Report{Health: userca.HealthMissing},
			want:   "HTTPS was requested",
		},
		{
			name:   "broken owned state warns without intent",
			list:   noIntent,
			report: userca.Report{Health: userca.HealthMismatchedMaterial},
			want:   "mismatched-material",
		},
		{
			name:      "near expiry stays ready with renewal warning",
			list:      noIntent,
			authority: &userca.Authority{},
			report:    userca.Report{Health: userca.HealthExpiringSoon, Expires: expiry},
			want:      "expires soon",
		},
		{
			name:   "inspection failure warns",
			list:   noIntent,
			report: userca.Report{Health: userca.HealthUnknown},
			err:    context.DeadlineExceeded,
			want:   "could not be assessed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpsWarningDiagnostics(httpsReadinessWarnings(tt.list.UpstreamList(), tt.authority, tt.report, tt.err))
			if !strings.Contains(got, tt.want) {
				t.Fatalf("warning = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func httpsWarningDiagnostics(warnings []HTTPSWarningDetail) string {
	var diagnostics []string
	for _, warning := range warnings {
		diagnostics = append(diagnostics, warning.Diagnostic)
	}
	return strings.Join(diagnostics, "\n")
}

func upstreamListForTrafficTest(t *testing.T, contents string) liveconfig.Snapshot {
	t.Helper()
	_, snapshot, _ := createTrafficConfig(t, contents)
	return snapshot
}

type trafficTestTrustStore struct {
	records   []userca.TrustedCertificate
	trustErr  error
	removeErr error
}

func (s *trafficTestTrustStore) TrustedCertificates(context.Context) ([]userca.TrustedCertificate, error) {
	return append([]userca.TrustedCertificate(nil), s.records...), nil
}

func (s *trafficTestTrustStore) Trust(_ context.Context, certificatePEM []byte) error {
	fingerprint, err := userca.SHA1Fingerprint(certificatePEM)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certificatePEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	s.records = append(s.records, userca.TrustedCertificate{
		Fingerprint:    fingerprint,
		CertificatePEM: certificatePEM,
		ExpiresAt:      certificate.NotAfter,
	})
	return s.trustErr
}

func (s *trafficTestTrustStore) Remove(_ context.Context, fingerprints []string) error {
	if s.removeErr != nil {
		return s.removeErr
	}
	remove := map[string]bool{}
	for _, fingerprint := range fingerprints {
		remove[fingerprint] = true
	}
	var kept []userca.TrustedCertificate
	for _, record := range s.records {
		if !remove[record.Fingerprint] {
			kept = append(kept, record)
		}
	}
	s.records = kept
	return nil
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

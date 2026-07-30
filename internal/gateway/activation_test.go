package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

func TestExecuteStartBindsCollectiveConsentToForeignPACState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	upstreamPath := filepath.Join(configDir, "upstreams.txt")
	if err := os.WriteFile(upstreamPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	settings := &lifecycleTestSystemSettings{states: []managedpac.ServiceSnapshot{
		{ServiceName: "Wi-Fi", Enabled: true, PACURL: "http://corp.example/a.pac"},
		{ServiceName: "Ethernet"},
	}}
	lifecycle, err := newLifecycle(settings, emptyTestTrustStore{}, newCoordinator(filepath.Join(configDir, "runtime")), "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind != StartResultConsentRequired || first.PACReplacementConsent == nil {
		t.Fatalf("first start = %#v", first)
	}
	detail := first.PACReplacementConsent
	if len(detail.CurrentPACState) != 2 {
		t.Fatalf("managed service detail = %#v", detail.CurrentPACState)
	}
	if !detail.CurrentPACState[1].ReplacementConsentRequired || detail.CurrentPACState[0].ReplacementConsentRequired {
		t.Fatalf("replacement consent markers = %#v", detail.CurrentPACState)
	}
	if detail.Fingerprint == "" {
		t.Fatal("consent fingerprint is empty")
	}

	settings.states[1].PACURL = "http://corp.example/b.pac"
	changed, err := lifecycle.ExecuteStart(context.Background(), StartRequest{
		PACReplacementConsent: &PACReplacementConsentInput{Accepted: true, Fingerprint: detail.Fingerprint},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Kind != StartResultConsentRequired || changed.PACReplacementConsent == nil {
		t.Fatalf("changed-state retry = %#v", changed)
	}
	if changed.PACReplacementConsent.Fingerprint == detail.Fingerprint {
		t.Fatal("changed foreign PAC URL retained old consent fingerprint")
	}
	if settings.applied != 0 {
		t.Fatalf("state-mismatched consent applied PAC %d times", settings.applied)
	}
}

func TestExecuteStartReportsEarlyCleanupFailureAsStructuredOutcome(t *testing.T) {
	settings := &lifecycleTestSystemSettings{
		states: []managedpac.ServiceSnapshot{{
			ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1/seamless-cors.pac", Enabled: true,
		}},
		clearErr: errors.New("cleanup denied"),
	}
	lifecycle, err := newLifecycle(settings, emptyTestTrustStore{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StartResultCleanupFailed {
		t.Fatalf("start kind = %s, want %s", result.Kind, StartResultCleanupFailed)
	}
	if len(result.CleanupFailures) != 1 || result.CleanupFailures[0].Subject != CleanupSubjectManagedPAC {
		t.Fatalf("cleanup failures = %#v", result.CleanupFailures)
	}
}

func TestInstallDoesNotReadOrBootstrapLiveConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	lifecycle, err := newLifecycle(
		&lifecycleTestSystemSettings{},
		emptyTestTrustStore{},
		newCoordinator(filepath.Join(home, ".seamless-cors", "runtime")),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := lifecycle.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	upstreamPath := filepath.Join(home, ".seamless-cors", "upstreams.txt")
	if _, err := os.Stat(upstreamPath); !os.IsNotExist(err) {
		t.Fatalf("CA install created or inspected Live Configuration: %v", err)
	}
}

func TestInstallRecoversHTTPSReadinessInActiveRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, snapshot, _ := createTrafficConfigAtCurrentHome(t, "api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	if err := engine.SetInitialHTTPSReadiness(nil, userca.Report{Health: userca.HealthMissing}, nil); err != nil {
		t.Fatal(err)
	}

	store := &trafficTestTrustStore{}
	settings := &lifecycleTestSystemSettings{states: []managedpac.ServiceSnapshot{{
		ServiceName: "Wi-Fi",
		Enabled:     true,
		PACURL:      engine.PACURL(),
	}}}
	pac, err := managedpac.Prepare(settings, []string{"Wi-Fi"}, engine.PACURL())
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := newLifecycle(
		settings,
		store,
		newCoordinator(filepath.Join(home, ".seamless-cors", "runtime")),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: engine, pac: pac, phase: runtimePhaseRunning}
	initialPACURL := engine.PACURL()

	result, err := lifecycle.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != InstallResultInstalled {
		t.Fatalf("install kind = %s", result.Kind)
	}
	state := engine.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessReady {
		t.Fatalf("runtime state after install = %#v", state)
	}
	if engine.PACURL() == initialPACURL || pac.CurrentURL() != engine.PACURL() {
		t.Fatalf("PAC recovery was not refreshed: initial=%q engine=%q session=%q", initialPACURL, engine.PACURL(), pac.CurrentURL())
	}
}

func TestInstallPublishesCandidateCleanupFailureWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, snapshot, _ := createTrafficConfigAtCurrentHome(t, "https://api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	if err := engine.SetInitialHTTPSReadiness(nil, userca.Report{Health: userca.HealthMissing}, nil); err != nil {
		t.Fatal(err)
	}
	trustErr := errors.New("trust failed after changing state")
	cleanupErr := errors.New("candidate cleanup denied")
	store := &trafficTestTrustStore{trustErr: trustErr, removeErr: cleanupErr}
	lifecycle, err := newLifecycle(
		&lifecycleTestSystemSettings{},
		store,
		newCoordinator(filepath.Join(home, ".seamless-cors", "runtime")),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: engine, phase: runtimePhaseRunning}

	_, err = lifecycle.Install(context.Background())
	if !errors.Is(err, trustErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Install error = %v", err)
	}
	if !hasHTTPSWarning(engine.snapshot().HTTPSWarnings, HTTPSWarningNonActiveCleanupFailed) {
		t.Fatalf("runtime warnings = %#v", engine.snapshot().HTTPSWarnings)
	}
}

func TestInstallRotatesActiveUserCAWithoutStoppingHTTPS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, snapshot, _ := createTrafficConfigAtCurrentHome(t, "https://api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	store := &trafficTestTrustStore{}
	caDir, err := userca.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	first, firstResult, err := userca.Ensure(caDir, store)
	if err != nil {
		t.Fatal(err)
	}
	firstFingerprint, _ := first.Fingerprint()
	if err := engine.SetInitialHTTPSReadiness(first, firstResult.Report, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.KeyPath, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := newLifecycle(
		&lifecycleTestSystemSettings{},
		store,
		newCoordinator(filepath.Join(home, ".seamless-cors", "runtime")),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: engine, phase: runtimePhaseRunning}

	result, err := lifecycle.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != InstallResultInstalled {
		t.Fatalf("install kind = %s", result.Kind)
	}
	state := engine.snapshot()
	if state.HTTPSInterception != HTTPSInterceptionActive || state.HTTPSReadiness != HTTPSReadinessReady {
		t.Fatalf("HTTPS was disrupted by rotation: %#v", state)
	}
	secondFingerprint, _ := engine.authority.Fingerprint()
	if secondFingerprint == firstFingerprint {
		t.Fatal("runtime did not adopt the rotated UserCA generation")
	}
	if len(store.records) != 2 {
		t.Fatalf("trusted authority overlap = %d, want 2", len(store.records))
	}
}

func TestLiveUninstallRequiresConfirmationThenRemovesAllUserCAs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, snapshot, _ := createTrafficConfigAtCurrentHome(t, "https://api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	store := &trafficTestTrustStore{}
	caDir, err := userca.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	authority, ensured, err := userca.Ensure(caDir, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetInitialHTTPSReadiness(authority, ensured.Report, nil); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := newLifecycle(
		&lifecycleTestSystemSettings{},
		store,
		newCoordinator(filepath.Join(home, ".seamless-cors", "runtime")),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: engine, phase: runtimePhaseRunning}

	first, err := lifecycle.Uninstall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind != UninstallResultConsentRequired || first.ConsentFingerprint == "" {
		t.Fatalf("first uninstall = %#v", first)
	}
	if engine.snapshot().HTTPSInterception != HTTPSInterceptionActive || len(store.records) != 1 {
		t.Fatal("unconfirmed uninstall changed HTTPS or UserCA state")
	}

	second, err := lifecycle.UninstallWithConsent(context.Background(), first.ConsentFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if second.Kind != UninstallResultUninstalled {
		t.Fatalf("confirmed uninstall = %#v", second)
	}
	state := engine.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessNotReady || state.HTTPSInterception != HTTPSInterceptionInactive {
		t.Fatalf("runtime after uninstall = %#v", state)
	}
	if len(store.records) != 0 {
		t.Fatalf("trusted UserCAs remained: %d", len(store.records))
	}
	if _, err := os.Stat(caDir); !os.IsNotExist(err) {
		t.Fatalf("UserCA material remained: %v", err)
	}
}

func TestLiveUninstallReportsPACRefreshFailureAndRetryClearsWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, snapshot, _ := createTrafficConfigAtCurrentHome(t, "https://api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	store := &trafficTestTrustStore{}
	caDir, err := userca.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	authority, ensured, err := userca.Ensure(caDir, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetInitialHTTPSReadiness(authority, ensured.Report, nil); err != nil {
		t.Fatal(err)
	}
	refreshErr := errors.New("PAC update denied")
	settings := &lifecycleTestSystemSettings{
		states: []managedpac.ServiceSnapshot{{
			ServiceName: "Wi-Fi",
			Enabled:     true,
			PACURL:      engine.PACURL(),
		}},
		applyErr: refreshErr,
	}
	pac, err := managedpac.Prepare(settings, []string{"Wi-Fi"}, engine.PACURL())
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := newLifecycle(
		settings,
		store,
		newCoordinator(filepath.Join(home, ".seamless-cors", "runtime")),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: engine, pac: pac, phase: runtimePhaseRunning}

	consent, err := lifecycle.Uninstall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := lifecycle.UninstallWithConsent(context.Background(), consent.ConsentFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != UninstallResultPACRefreshFailed ||
		!hasHTTPSWarning(result.Warnings, HTTPSWarningPACRefreshFailed) {
		t.Fatalf("partial uninstall result = %#v", result)
	}
	if !hasHTTPSWarning(engine.snapshot().HTTPSWarnings, HTTPSWarningPACRefreshFailed) {
		t.Fatalf("runtime warnings = %#v", engine.snapshot().HTTPSWarnings)
	}

	settings.applyErr = nil
	retry, err := lifecycle.Uninstall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if retry.Kind != UninstallResultAlreadyAbsent {
		t.Fatalf("retry result = %#v", retry)
	}
	state := engine.snapshot()
	if hasHTTPSWarning(state.HTTPSWarnings, HTTPSWarningPACRefreshFailed) ||
		hasHTTPSWarning(state.HTTPSWarnings, HTTPSWarningUninstallIncomplete) {
		t.Fatalf("successful retry retained stale warnings: %#v", state.HTTPSWarnings)
	}
	if pac.CurrentURL() != engine.PACURL() {
		t.Fatalf("PAC retry did not converge: session %q, runtime %q", pac.CurrentURL(), engine.PACURL())
	}
}

func TestLifecyclePublishesLiveHTTPSWarningSnapshots(t *testing.T) {
	config, snapshot, _ := createTrafficConfig(t, "api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	if err := engine.SetInitialHTTPSReadiness(nil, userca.Report{Health: userca.HealthMissing}, nil); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := newLifecycle(
		&lifecycleTestSystemSettings{},
		&trafficTestTrustStore{},
		newCoordinator(t.TempDir()),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRuntime{engine: engine, phase: runtimePhaseRunning}
	lifecycle.runtime = active
	published := make(chan []HTTPSWarningDetail, 2)
	lifecycle.SetHTTPSWarningsChanged(func(warnings []HTTPSWarningDetail) {
		published <- warnings
	})
	<-published // Initial current snapshot.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go lifecycle.watchHTTPSWarningUpdates(ctx, active)

	engine.SetPACRefreshError(errors.New("refresh failed"))
	select {
	case warnings := <-published:
		if !hasHTTPSWarning(warnings, HTTPSWarningPACRefreshFailed) {
			t.Fatalf("published warnings = %#v", warnings)
		}
	case <-time.After(time.Second):
		t.Fatal("live HTTPS warning snapshot was not published")
	}
}

func TestStartReportsUnmetHTTPSIntentWithoutInstallingUserCA(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "upstreams.txt"), []byte("https://api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{states: []managedpac.ServiceSnapshot{{ServiceName: "Wi-Fi"}}}
	lifecycle, err := newLifecycle(
		settings,
		&trafficTestTrustStore{},
		newCoordinator(filepath.Join(configDir, "runtime")),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = lifecycle.Stop(context.Background())
	})
	if result.Kind != StartResultStarted || result.Guidance == nil {
		t.Fatalf("start result = %#v", result)
	}
	if result.Guidance.HTTPSReadiness != HTTPSReadinessNotReady || !result.Guidance.HTTPSIntent {
		t.Fatalf("HTTPS guidance = %#v", result.Guidance)
	}
	if !hasHTTPSWarning(result.Guidance.HTTPSWarnings, HTTPSWarningUnmetIntent) {
		t.Fatal("start omitted Unmet HTTPS Intent warning")
	}
	if len(settings.states) == 0 || settings.applied != 1 {
		t.Fatalf("gateway did not continue through PAC activation: applied=%d states=%#v", settings.applied, settings.states)
	}
}

func TestStartEnablesHTTPSFromInstalledUserCAWithoutHTTPSIntent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "upstreams.txt"), []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &trafficTestTrustStore{}
	caDir, err := userca.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := userca.Ensure(caDir, store); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{states: []managedpac.ServiceSnapshot{{ServiceName: "Wi-Fi"}}}
	lifecycle, err := newLifecycle(
		settings,
		store,
		newCoordinator(filepath.Join(configDir, "runtime")),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = lifecycle.Stop(context.Background())
	})
	if result.Kind != StartResultStarted || result.Guidance == nil {
		t.Fatalf("start result = %#v", result)
	}
	if result.Guidance.HTTPSReadiness != HTTPSReadinessReady || result.Guidance.HTTPSIntent {
		t.Fatalf("HTTPS guidance = %#v", result.Guidance)
	}
	if len(result.Guidance.HTTPSWarnings) != 0 {
		t.Fatalf("HTTPS warnings = %#v", result.Guidance.HTTPSWarnings)
	}
}

type lifecycleTestSystemSettings struct {
	states   []managedpac.ServiceSnapshot
	applied  int
	applyErr error
	stateErr error
	clearErr error
	cleared  int
}

func (f *lifecycleTestSystemSettings) Apply(_ context.Context, _ string, services []string) (managedpac.ApplyResult, error) {
	f.applied++
	if f.applyErr != nil {
		return managedpac.ApplyResult{}, f.applyErr
	}
	return managedpac.ApplyResult{AppliedServices: append([]string(nil), services...)}, nil
}

func (f *lifecycleTestSystemSettings) Snapshot(context.Context) ([]managedpac.ServiceSnapshot, error) {
	if f.stateErr != nil {
		return nil, f.stateErr
	}
	return append([]managedpac.ServiceSnapshot(nil), f.states...), nil
}

func (f *lifecycleTestSystemSettings) ClearIfUnchanged(_ context.Context, expected []managedpac.ServiceSnapshot) error {
	f.cleared++
	if f.clearErr != nil {
		return f.clearErr
	}
	for idx, state := range f.states {
		for _, want := range expected {
			if state == want {
				f.states[idx].Enabled = false
			}
		}
	}
	return nil
}

type emptyTestTrustStore struct{}

func (emptyTestTrustStore) TrustedCertificates(context.Context) ([]userca.TrustedCertificate, error) {
	return nil, nil
}
func (emptyTestTrustStore) Trust(context.Context, []byte) error    { return nil }
func (emptyTestTrustStore) Remove(context.Context, []string) error { return nil }

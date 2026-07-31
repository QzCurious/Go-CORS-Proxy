package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

func TestExecuteStartBindsCollectiveConsentToForeignPACState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "upstreams.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{states: []managedpac.ServiceSnapshot{
		{ServiceName: "Wi-Fi", Enabled: true, PACURL: "http://corp.example/a.pac"},
		{ServiceName: "Ethernet"},
	}}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(filepath.Join(configDir, "runtime")), "")
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
	settings.states[1].PACURL = "http://corp.example/b.pac"
	changed, err := lifecycle.ExecuteStart(context.Background(), StartRequest{
		PACReplacementConsent: &PACReplacementConsentInput{Accepted: true, Fingerprint: detail.Fingerprint},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Kind != StartResultConsentRequired || changed.PACReplacementConsent.Fingerprint == detail.Fingerprint {
		t.Fatalf("changed-state retry = %#v", changed)
	}
}

func TestExecuteStartReportsEarlyCleanupFailureAsStructuredOutcome(t *testing.T) {
	settings := &lifecycleTestSystemSettings{
		states: []managedpac.ServiceSnapshot{{
			ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1/seamless-cors.pac", Enabled: true,
		}},
		clearErr: errors.New("cleanup denied"),
	}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})

	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StartResultCleanupFailed || len(result.CleanupFailures) != 1 {
		t.Fatalf("start result = %#v", result)
	}
}

func TestInstallUsesOnlyUserCAAndDoesNotBootstrapLiveConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ca := &fakeUserCA{
		installResult: userCAInstallResult{current: testUserCASnapshot(t, time.Now().Add(24*time.Hour), false), changed: true},
	}
	lifecycle, err := newLifecycle(
		&lifecycleTestSystemSettings{},
		ca,
		newCoordinator(filepath.Join(home, ".seamless-cors", "runtime")),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := lifecycle.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ca.installCalls != 1 {
		t.Fatalf("UserCA install calls = %d", ca.installCalls)
	}
	if _, err := os.Stat(filepath.Join(home, ".seamless-cors", "upstreams.txt")); !os.IsNotExist(err) {
		t.Fatalf("install touched Live Configuration: %v", err)
	}
}

func TestInstallRecoversHTTPSInActiveRuntime(t *testing.T) {
	config, snapshot, _ := createTrafficConfig(t, "https://api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	if err := engine.SetInitialHTTPSReadiness(userCASnapshot{}, nil); err != nil {
		t.Fatal(err)
	}
	installed := testUserCASnapshot(t, time.Now().Add(24*time.Hour), false)
	ca := &fakeUserCA{installResult: userCAInstallResult{current: installed, changed: true}}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: engine, phase: runtimePhaseRunning}

	result, err := lifecycle.Install(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != InstallResultInstalled || engine.snapshot().HTTPSInterception != HTTPSInterceptionActive {
		t.Fatalf("install result = %#v runtime = %#v", result, engine.snapshot())
	}
}

func TestInstallReturnsPartialSuccessWhenRuntimeCannotAdopt(t *testing.T) {
	config, snapshot, _ := createTrafficConfig(t, "https://api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	if err := engine.SetInitialHTTPSReadiness(userCASnapshot{}, nil); err != nil {
		t.Fatal(err)
	}
	invalidTLS := userCASnapshot{usable: true, expiresAt: time.Now().Add(time.Hour)}
	ca := &fakeUserCA{installResult: userCAInstallResult{current: invalidTLS, changed: true}}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: engine, phase: runtimePhaseRunning}

	result, err := lifecycle.Install(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != InstallResultRuntimeAdoptionFailed || lifecycle.userCASnapshot.usable != true {
		t.Fatalf("partial install result = %#v latched = %#v", result, lifecycle.userCASnapshot)
	}
}

func TestCAAdmissionFailsFastAndStatusReportsMutating(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	ca := &fakeUserCA{
		install: func(ctx context.Context) (userCAInstallResult, error) {
			if ctx.Err() != nil {
				return userCAInstallResult{}, ctx.Err()
			}
			close(entered)
			<-release
			return userCAInstallResult{current: userCASnapshot{}, changed: true}, nil
		},
	}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := lifecycle.Install(context.Background())
		done <- err
	}()
	<-entered

	if _, err := lifecycle.Install(context.Background()); !errors.Is(err, errCAOperationInProgress) {
		t.Fatalf("competing install error = %v", err)
	}
	status, err := lifecycle.Status(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if status.InstalledCA.Health != CAHealthMutating {
		t.Fatalf("status health = %s", status.InstalledCA.Health)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTransientOwnerRejectsStartBeforeCAMutationAdmission(t *testing.T) {
	lifecycle, err := newLifecycleUninspected(
		&lifecycleTestSystemSettings{},
		&fakeUserCA{},
		newCoordinator(t.TempDir()),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.transientOwner = true

	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})

	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StartResultStartAlreadyMutating {
		t.Fatalf("transient start result = %s", result.Kind)
	}
}

func TestAdmittedCAOperationIgnoresRequestCancellation(t *testing.T) {
	requestCtx, cancel := context.WithCancel(context.Background())
	observed := make(chan error, 1)
	ca := &fakeUserCA{
		install: func(ctx context.Context) (userCAInstallResult, error) {
			cancel()
			observed <- ctx.Err()
			return userCAInstallResult{}, nil
		},
	}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := lifecycle.Install(requestCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-observed; err != nil {
		t.Fatalf("owner-owned operation inherited request cancellation: %v", err)
	}
}

func TestLiveUninstallRequiresConsentThenDeactivatesBeforeRemoval(t *testing.T) {
	config, snapshot, _ := createTrafficConfig(t, "https://api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	installed := testUserCASnapshot(t, time.Now().Add(24*time.Hour), false)
	if err := engine.SetInitialHTTPSReadiness(installed, nil); err != nil {
		t.Fatal(err)
	}
	var inactiveDuringUninstall bool
	ca := &fakeUserCA{
		snapshot: installed,
		uninstall: func(context.Context) (userCAUninstallResult, error) {
			inactiveDuringUninstall = engine.snapshot().HTTPSInterception == HTTPSInterceptionInactive
			return userCAUninstallResult{current: userCASnapshot{}, changed: true}, nil
		},
	}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: engine, phase: runtimePhaseRunning}

	first, err := lifecycle.Uninstall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind != UninstallResultConsentRequired || ca.uninstallCalls != 0 {
		t.Fatalf("unconfirmed uninstall = %#v calls %d", first, ca.uninstallCalls)
	}
	second, err := lifecycle.UninstallWithConsent(context.Background(), first.ConsentFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if second.Kind != UninstallResultUninstalled || !inactiveDuringUninstall {
		t.Fatalf("accepted uninstall = %#v inactive during removal %t", second, inactiveDuringUninstall)
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
	ca := &fakeUserCA{}
	lifecycle, err := newLifecycle(settings, ca, newCoordinator(filepath.Join(configDir, "runtime")), "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = lifecycle.Stop(context.Background()) })
	if result.Kind != StartResultStarted || result.Guidance == nil ||
		result.Guidance.HTTPSReadiness != HTTPSReadinessNotReady ||
		!hasHTTPSWarning(result.Guidance.HTTPSWarnings, HTTPSWarningUnmetIntent) {
		t.Fatalf("start result = %#v", result)
	}
	if ca.installCalls != 0 {
		t.Fatal("start implicitly installed UserCA")
	}
}

func TestStartUsesFreshInstalledUserCASnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "upstreams.txt"), []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installed := testUserCASnapshot(t, time.Now().Add(24*time.Hour), false)
	ca := &fakeUserCA{snapshot: installed}
	settings := &lifecycleTestSystemSettings{states: []managedpac.ServiceSnapshot{{ServiceName: "Wi-Fi"}}}
	lifecycle, err := newLifecycle(settings, ca, newCoordinator(filepath.Join(configDir, "runtime")), "")
	if err != nil {
		t.Fatal(err)
	}
	inspectionsAtConstruction := ca.inspectCalls

	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = lifecycle.Stop(context.Background()) })
	if result.Kind != StartResultStarted || result.Guidance.HTTPSReadiness != HTTPSReadinessReady {
		t.Fatalf("start result = %#v", result)
	}
	if ca.inspectCalls <= inspectionsAtConstruction {
		t.Fatal("start reused construction-time UserCA inspection")
	}
}

type fakeUserCA struct {
	mu              sync.Mutex
	snapshot        userCASnapshot
	inspectErr      error
	installResult   userCAInstallResult
	installErr      error
	uninstallResult userCAUninstallResult
	uninstallErr    error
	install         func(context.Context) (userCAInstallResult, error)
	uninstall       func(context.Context) (userCAUninstallResult, error)
	inspectCalls    int
	installCalls    int
	uninstallCalls  int
}

func (f *fakeUserCA) Inspect(context.Context) (userCASnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inspectCalls++
	return f.snapshot, f.inspectErr
}

func (f *fakeUserCA) Install(ctx context.Context) (userCAInstallResult, error) {
	f.mu.Lock()
	f.installCalls++
	operation := f.install
	result, err := f.installResult, f.installErr
	f.mu.Unlock()
	if operation != nil {
		return operation(ctx)
	}
	return result, err
}

func (f *fakeUserCA) Uninstall(ctx context.Context) (userCAUninstallResult, error) {
	f.mu.Lock()
	f.uninstallCalls++
	operation := f.uninstall
	result, err := f.uninstallResult, f.uninstallErr
	f.mu.Unlock()
	if operation != nil {
		return operation(ctx)
	}
	return result, err
}

type emptyTestUserCA struct{}

func (emptyTestUserCA) Inspect(context.Context) (userCASnapshot, error) {
	return userCASnapshot{}, nil
}
func (emptyTestUserCA) Install(context.Context) (userCAInstallResult, error) {
	return userCAInstallResult{}, nil
}
func (emptyTestUserCA) Uninstall(context.Context) (userCAUninstallResult, error) {
	return userCAUninstallResult{}, nil
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
	for index, state := range f.states {
		for _, want := range expected {
			if state == want {
				f.states[index].Enabled = false
			}
		}
	}
	return nil
}

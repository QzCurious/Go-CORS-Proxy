package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

func TestExecuteStartFixesConsentSelectedServicesWithoutBindingPACURLs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "upstreams.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{services: []managedpac.Service{
		{Name: "Wi-Fi", Enabled: true, URL: "http://corp.example/a.pac", Ownership: managedpac.OwnershipForeign},
		{Name: "Ethernet", Ownership: managedpac.OwnershipEmpty},
		{Name: "USB", Ownership: managedpac.OwnershipEmpty},
	}}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(filepath.Join(configDir, "runtime")), "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind != StartResultConsentRequired || first.ManagedPACConsent == nil {
		t.Fatalf("first start = %#v", first)
	}
	detail := first.ManagedPACConsent
	installResult := managedpac.NewInstallResult(
		managedpac.NewRuntimeState([]string{"Ethernet", "USB"}, "ignored by assertion"),
		[]string{"USB"},
		[]managedpac.Warning{{Kind: managedpac.WarningDrift, ServiceName: "Ethernet", Diagnostic: "foreign PAC state is active"}},
	)
	settings.installResult = &installResult
	changed, err := lifecycle.ExecuteStart(context.Background(), StartRequest{
		ManagedPACConsent: &ManagedPACConsentInput{
			ServiceNames: detail.ProposedServices,
			Fingerprint:  detail.Fingerprint,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Kind != StartResultStarted || changed.Guidance == nil {
		t.Fatalf("accepted retry = %#v", changed)
	}
	if got := changed.Guidance.ManagedPACServices; len(got) != 2 || got[0] != "Ethernet" || got[1] != "USB" {
		t.Fatalf("fixed service set = %v", got)
	}
	if len(changed.Guidance.ManagedPACWarnings) != 1 || changed.Guidance.ManagedPACWarnings[0].ServiceName != "Ethernet" {
		t.Fatalf("managed PAC warnings = %#v", changed.Guidance.ManagedPACWarnings)
	}
	t.Cleanup(func() { _, _ = lifecycle.Stop(context.Background()) })
}

func TestExecuteStartReportsEarlyCleanupFailureAsStructuredOutcome(t *testing.T) {
	settings := &lifecycleTestSystemSettings{
		clearErr: errors.New("cleanup denied"),
	}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := executeAcceptedStart(lifecycle)

	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StartResultCleanupFailed || len(result.CleanupFailures) != 1 {
		t.Fatalf("start result = %#v", result)
	}
}

func TestExecuteStartStopsWhenNoManageablePACServiceExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settings := &lifecycleTestSystemSettings{services: []managedpac.Service{{
		Name: "Wi-Fi", URL: "http://corp.example/proxy.pac", Enabled: true, Ownership: managedpac.OwnershipForeign,
	}}}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StartResultNoManageablePACServices || lifecycle.RuntimeActive() {
		t.Fatalf("start result = %#v runtime active = %t", result, lifecycle.RuntimeActive())
	}
	if result.ManagedPACConsent == nil || len(result.ManagedPACConsent.ProposedServices) != 0 {
		t.Fatalf("service assessment = %#v", result.ManagedPACConsent)
	}
	if settings.applied != 0 {
		t.Fatalf("PAC writes = %d, want none", settings.applied)
	}
}

func TestExecuteStartReportsWarningsWhenManagedPACInstallationReachesNoService(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settings := &lifecycleTestSystemSettings{
		services:   []managedpac.Service{{Name: "Wi-Fi", Ownership: managedpac.OwnershipEmpty}},
		installErr: errors.New("managed PAC install updated no services"),
	}
	installResult := managedpac.NewInstallResult(
		managedpac.NewRuntimeState([]string{"Wi-Fi"}, ""),
		nil,
		[]managedpac.Warning{{Kind: managedpac.WarningUpdateFailed, ServiceName: "Wi-Fi", Diagnostic: "PAC write denied"}},
	)
	settings.installResult = &installResult
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := executeAcceptedStart(lifecycle)

	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StartResultManagedPACInstallationFailed || len(result.ManagedPACWarnings) != 1 {
		t.Fatalf("start result = %#v", result)
	}
	if warning := result.ManagedPACWarnings[0]; warning.ServiceName != "Wi-Fi" || warning.Kind != ManagedPACWarningUpdateFailed {
		t.Fatalf("Managed PAC warning = %#v", warning)
	}
}

func TestInstallUsesOnlyUserCAAndDoesNotBootstrapLiveConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ca := &fakeUserCA{
		installResult: userca.NewInstallResult(testUserCASnapshot(t, time.Now().Add(24*time.Hour), false), true),
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
	if err := engine.SetInitialHTTPSReadiness(userca.Snapshot{}, nil); err != nil {
		t.Fatal(err)
	}
	installed := testUserCASnapshot(t, time.Now().Add(24*time.Hour), false)
	ca := &fakeUserCA{installResult: userca.NewInstallResult(installed, true)}
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
	if err := engine.SetInitialHTTPSReadiness(userca.Snapshot{}, nil); err != nil {
		t.Fatal(err)
	}
	installed := testUserCASnapshot(t, time.Now().Add(time.Hour), false)
	ca := &fakeUserCA{installResult: userca.NewInstallResult(installed, true)}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: engine, phase: runtimePhaseRunning}
	engine.mu.Lock()
	engine.proxyCore = nil
	engine.mu.Unlock()

	result, err := lifecycle.Install(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != InstallResultRuntimeAdoptionFailed || !lifecycle.userCASnapshot.Usable() {
		t.Fatalf("partial install result = %#v latched = %#v", result, lifecycle.userCASnapshot)
	}
}

func TestManagedPACReconciliationUpdatesLatchedWarningsWithoutLosingFixedSet(t *testing.T) {
	config, snapshot, _ := createTrafficConfig(t, "api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	if err := engine.SetInitialHTTPSReadiness(userca.Snapshot{}, nil); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{
		reconcileWarnings: []managedpac.Warning{{Kind: managedpac.WarningDrift, ServiceName: "Wi-Fi", Diagnostic: "foreign PAC state is active"}},
	}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRuntime{
		engine: engine,
		phase:  runtimePhaseRunning,
		managedPAC: &managedPACRuntime{state: managedpac.NewRuntimeState(
			[]string{"Wi-Fi"},
			"http://127.0.0.1/seamless-cors.pac?v=1",
		)},
	}
	lifecycle.runtime = active
	engine.mu.Lock()
	engine.pacVersion = 2
	nextURL := engine.pacURL(2)
	engine.mu.Unlock()

	lifecycle.requestPACReconciliation(active, nextURL)
	status, err := lifecycle.Status(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if status.Runtime == nil || !status.Runtime.ManagedPACActive || len(status.Runtime.ManagedPACServices) != 1 || status.Runtime.ManagedPACServices[0] != "Wi-Fi" {
		t.Fatalf("Managed PAC runtime status = %#v", status.Runtime)
	}
	if len(status.Runtime.ManagedPACWarnings) != 1 || status.Runtime.ManagedPACWarnings[0].Kind != ManagedPACWarningDrift {
		t.Fatalf("Managed PAC warnings = %#v", status.Runtime.ManagedPACWarnings)
	}
}

func TestManagedPACReconciliationSuppressesStaleRuntimeURL(t *testing.T) {
	config, snapshot, _ := createTrafficConfig(t, "api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	settings := &lifecycleTestSystemSettings{}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRuntime{
		engine:     engine,
		phase:      runtimePhaseRunning,
		managedPAC: &managedPACRuntime{state: managedpac.NewRuntimeState([]string{"Wi-Fi"}, engine.pacURL(1))},
	}
	lifecycle.runtime = active
	engine.mu.Lock()
	engine.pacVersion = 3
	staleURL := engine.pacURL(2)
	latestURL := engine.pacURL(3)
	engine.mu.Unlock()

	lifecycle.requestPACReconciliation(active, staleURL)
	lifecycle.requestPACReconciliation(active, latestURL)

	if got, want := settings.requestedURLs, []string{latestURL}; !slices.Equal(got, want) {
		t.Fatalf("reconciliation requests = %v, want %v", got, want)
	}
}

func TestManagedPACReconciliationFailurePreservesWarningSnapshot(t *testing.T) {
	config, snapshot, _ := createTrafficConfig(t, "api.example.test\n")
	engine, err := newRuntime(config, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	settings := &lifecycleTestSystemSettings{reconcileErr: errors.New("inspection denied")}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	initial := []ManagedPACWarningDetail{{Kind: ManagedPACWarningDrift, ServiceName: "Wi-Fi", Diagnostic: "foreign PAC state is active"}}
	active := &activeRuntime{
		engine: engine,
		phase:  runtimePhaseRunning,
		managedPAC: &managedPACRuntime{
			state:    managedpac.NewRuntimeState([]string{"Wi-Fi"}, engine.pacURL(1)),
			warnings: initial,
		},
	}
	lifecycle.runtime = active
	engine.mu.Lock()
	engine.pacVersion = 2
	nextURL := engine.pacURL(2)
	engine.mu.Unlock()

	lifecycle.requestPACReconciliation(active, nextURL)

	if !slices.Equal(active.managedPAC.warnings, initial) {
		t.Fatalf("warnings after reconciliation failure = %#v", active.managedPAC.warnings)
	}
}

func TestCAAdmissionFailsFastAndStatusReportsMutating(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	ca := &fakeUserCA{
		install: func(ctx context.Context) (userca.InstallResult, error) {
			if ctx.Err() != nil {
				return userca.InstallResult{}, ctx.Err()
			}
			close(entered)
			<-release
			return userca.NewInstallResult(userca.Snapshot{}, true), nil
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

	competing, err := lifecycle.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if competing.Kind != InstallResultAlreadyMutating {
		t.Fatalf("competing install result = %#v", competing)
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

	result, err := executeAcceptedStart(lifecycle)

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
		install: func(ctx context.Context) (userca.InstallResult, error) {
			cancel()
			observed <- ctx.Err()
			return userca.InstallResult{}, nil
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
		uninstall: func(context.Context) (userca.UninstallResult, error) {
			inactiveDuringUninstall = engine.snapshot().HTTPSInterception == HTTPSInterceptionInactive
			return userca.NewUninstallResult(userca.Snapshot{}, true), nil
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
	settings := &lifecycleTestSystemSettings{services: []managedpac.Service{{Name: "Wi-Fi", Ownership: managedpac.OwnershipEmpty}}}
	ca := &fakeUserCA{}
	lifecycle, err := newLifecycle(settings, ca, newCoordinator(filepath.Join(configDir, "runtime")), "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := executeAcceptedStart(lifecycle)
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
	settings := &lifecycleTestSystemSettings{services: []managedpac.Service{{Name: "Wi-Fi", Ownership: managedpac.OwnershipEmpty}}}
	lifecycle, err := newLifecycle(settings, ca, newCoordinator(filepath.Join(configDir, "runtime")), "")
	if err != nil {
		t.Fatal(err)
	}
	inspectionsAtConstruction := ca.inspectCalls

	result, err := executeAcceptedStart(lifecycle)
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
	snapshot        userca.Snapshot
	inspectErr      error
	installResult   userca.InstallResult
	installErr      error
	uninstallResult userca.UninstallResult
	uninstallErr    error
	install         func(context.Context) (userca.InstallResult, error)
	uninstall       func(context.Context) (userca.UninstallResult, error)
	inspectCalls    int
	installCalls    int
	uninstallCalls  int
}

func (f *fakeUserCA) Inspect(context.Context) (userca.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inspectCalls++
	return f.snapshot, f.inspectErr
}

func (f *fakeUserCA) Install(ctx context.Context) (userca.InstallResult, error) {
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

func (f *fakeUserCA) Uninstall(ctx context.Context) (userca.UninstallResult, error) {
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

func (emptyTestUserCA) Inspect(context.Context) (userca.Snapshot, error) {
	return userca.Snapshot{}, nil
}
func (emptyTestUserCA) Install(context.Context) (userca.InstallResult, error) {
	return userca.InstallResult{}, nil
}
func (emptyTestUserCA) Uninstall(context.Context) (userca.UninstallResult, error) {
	return userca.UninstallResult{}, nil
}

type lifecycleTestSystemSettings struct {
	services          []managedpac.Service
	applied           int
	installResult     *managedpac.InstallResult
	installErr        error
	stateErr          error
	clearErr          error
	cleared           int
	reconcileWarnings []managedpac.Warning
	reconcileErr      error
	requestedURLs     []string
}

func (f *lifecycleTestSystemSettings) Inspect(context.Context) (managedpac.Snapshot, error) {
	if f.stateErr != nil {
		return managedpac.Snapshot{}, f.stateErr
	}
	return managedpac.NewSnapshot(f.services), nil
}

func (f *lifecycleTestSystemSettings) Install(_ context.Context, services []string, pacURL string) (managedpac.InstallResult, error) {
	f.applied++
	if f.installResult != nil {
		return *f.installResult, f.installErr
	}
	return managedpac.NewInstallResult(
		managedpac.NewRuntimeState(sortedUniqueServiceNames(services), pacURL),
		sortedUniqueServiceNames(services),
		nil,
	), f.installErr
}

func (f *lifecycleTestSystemSettings) RequestReconcile(_ managedpac.RuntimeState, pacURL string, complete func(managedpac.ReconcileResult)) {
	f.requestedURLs = append(f.requestedURLs, pacURL)
	if complete != nil {
		complete(managedpac.NewReconcileResult(f.reconcileWarnings, f.reconcileErr))
	}
}

func (f *lifecycleTestSystemSettings) Uninstall(context.Context) error {
	f.cleared++
	if f.clearErr != nil {
		return f.clearErr
	}
	return nil
}

func executeAcceptedStart(lifecycle *lifecycle) (StartResult, error) {
	first, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil || first.Kind != StartResultConsentRequired || first.ManagedPACConsent == nil {
		return first, err
	}
	return lifecycle.ExecuteStart(context.Background(), StartRequest{ManagedPACConsent: &ManagedPACConsentInput{
		ServiceNames: first.ManagedPACConsent.ProposedServices,
		Fingerprint:  first.ManagedPACConsent.Fingerprint,
	}})
}

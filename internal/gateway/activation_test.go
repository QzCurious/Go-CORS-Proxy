package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	consentResult, ok := first.(StartConsentRequired)
	if !ok {
		t.Fatalf("first start = %#v", first)
	}
	detail := consentResult.Consent
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
	started, ok := changed.(Started)
	if !ok {
		t.Fatalf("accepted retry = %#v", changed)
	}
	if got := started.Guidance.ManagedPACServices; len(got) != 2 || got[0] != "Ethernet" || got[1] != "USB" {
		t.Fatalf("fixed service set = %v", got)
	}
	if len(started.Guidance.ManagedPACWarnings) != 1 || started.Guidance.ManagedPACWarnings[0].ServiceName != "Ethernet" {
		t.Fatalf("managed PAC warnings = %#v", started.Guidance.ManagedPACWarnings)
	}
	t.Cleanup(func() { _, _ = lifecycle.Stop(context.Background()) })
}

func TestExecuteStartRequiresCreationConsentThenCreatesBeforePACConsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	lifecycle, err := newLifecycle(
		&lifecycleTestSystemSettings{services: []managedpac.Service{{Name: "Wi-Fi", Ownership: managedpac.OwnershipEmpty}}},
		emptyTestUserCA{}, newCoordinator(filepath.Join(home, "runtime")), "",
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	creation, ok := first.(StartUpstreamListCreationConsentRequired)
	if !ok {
		t.Fatalf("first = %#v", first)
	}
	second, err := lifecycle.ExecuteStart(context.Background(), StartRequest{UpstreamListCreationConsent: &UpstreamListCreationConsentInput{
		Decision: UpstreamListCreationAccepted, Fingerprint: creation.Consent.Fingerprint,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := second.(StartConsentRequired); !ok {
		t.Fatalf("second = %#v", second)
	}
	if _, err := os.Stat(creation.Consent.Path); err != nil {
		t.Fatalf("created file: %v", err)
	}
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
	cleanup, ok := result.(StartCleanupFailed)
	if !ok || len(cleanup.Failures) != 1 {
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

	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{UpstreamListCreationConsent: &UpstreamListCreationConsentInput{Decision: UpstreamListCreationDeclined}})
	if err != nil {
		t.Fatal(err)
	}
	noServices, ok := result.(StartNoManageablePACServices)
	if !ok || lifecycle.RuntimeActive() {
		t.Fatalf("start result = %#v runtime active = %t", result, lifecycle.RuntimeActive())
	}
	if len(noServices.Consent.ProposedServices) != 0 {
		t.Fatalf("service assessment = %#v", noServices.Consent)
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

	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{UpstreamListCreationConsent: &UpstreamListCreationConsentInput{Decision: UpstreamListCreationDeclined}, ManagedPACConsent: &ManagedPACConsentInput{ServiceNames: []string{"Wi-Fi"}, Fingerprint: pacConsentFingerprint([]string{"Wi-Fi"})}})

	if err != nil {
		t.Fatal(err)
	}
	failed, ok := result.(StartManagedPACInstallationFailed)
	if !ok || len(failed.Warnings) != 1 {
		t.Fatalf("start result = %#v", result)
	}
	if warning := failed.Warnings[0]; warning.ServiceName != "Wi-Fi" || warning.Kind != ManagedPACWarningUpdateFailed {
		t.Fatalf("Managed PAC warning = %#v", warning)
	}
}

func TestInstallUsesOnlyUserCAAndDoesNotBootstrapUpstreamList(t *testing.T) {
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
		t.Fatalf("install touched Upstream List source: %v", err)
	}
}

func TestInstallRecoversHTTPSInActiveRuntime(t *testing.T) {
	source, snapshot, path := createTrafficConfig(t, "https://api.example.test\n")
	engine, err := newRuntime(path, source, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	if err := engine.SetInitialHTTPSReadiness(userca.Assessment{}, nil); err != nil {
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

func TestDeadlineSignalReassessesAndWithdrawsUnusableHTTPS(t *testing.T) {
	source, upstreams, path := createTrafficConfig(t, "https://api.example.test\n")
	engine, err := newRuntime(path, source, upstreams)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	assessment := testUserCASnapshot(t, time.Now().Add(time.Hour), false)
	if err := engine.SetInitialHTTPSReadiness(assessment, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.DesiredStates():
	default:
	}
	ca := &fakeUserCA{}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRuntime{engine: engine, phase: runtimePhaseRunning}
	lifecycle.runtime = active

	lifecycle.handleHTTPSDeadline(active)

	state := engine.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessNotReady || state.HTTPSInterception != HTTPSInterceptionInactive {
		t.Fatalf("deadline state = %#v", state)
	}
	select {
	case desired := <-engine.DesiredStates():
		if desired.HTTPSInterception {
			t.Fatalf("deadline retained HTTPS PAC route: %#v", desired)
		}
	case <-time.After(time.Second):
		t.Fatal("deadline did not publish PAC withdrawal")
	}
}

func TestStaleDeadlineSignalLeavesFreshUsableHTTPSAlone(t *testing.T) {
	source, upstreams, path := createTrafficConfig(t, "https://api.example.test\n")
	engine, err := newRuntime(path, source, upstreams)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	assessment := testUserCASnapshot(t, time.Now().Add(time.Hour), false)
	if err := engine.SetInitialHTTPSReadiness(assessment, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.DesiredStates():
	default:
	}
	ca := &fakeUserCA{assessment: assessment}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRuntime{engine: engine, phase: runtimePhaseRunning}
	lifecycle.runtime = active

	lifecycle.handleHTTPSDeadline(active)

	state := engine.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessReady || state.HTTPSInterception != HTTPSInterceptionActive {
		t.Fatalf("stale deadline changed usable HTTPS: %#v", state)
	}
}

func TestDeadlineAssessmentFailureWithdrawsHTTPSAndReportsReadinessError(t *testing.T) {
	source, upstreams, path := createTrafficConfig(t, "https://api.example.test\n")
	engine, err := newRuntime(path, source, upstreams)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	assessment := testUserCASnapshot(t, time.Now().Add(time.Hour), false)
	if err := engine.SetInitialHTTPSReadiness(assessment, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.DesiredStates():
	default:
	}
	ca := &fakeUserCA{inspectErr: errors.New("trust store unavailable")}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRuntime{engine: engine, phase: runtimePhaseRunning}
	lifecycle.runtime = active

	lifecycle.handleHTTPSDeadline(active)

	state := engine.snapshot()
	if state.HTTPSReadiness != HTTPSReadinessNotReady || state.HTTPSInterception != HTTPSInterceptionInactive {
		t.Fatalf("assessment failure state = %#v", state)
	}
	if !strings.Contains(httpsWarningDiagnostics(state.HTTPSWarnings), "could not be assessed") {
		t.Fatalf("assessment failure warnings = %#v", state.HTTPSWarnings)
	}
	select {
	case desired := <-engine.DesiredStates():
		if desired.HTTPSInterception {
			t.Fatalf("assessment failure retained HTTPS PAC route: %#v", desired)
		}
	case <-time.After(time.Second):
		t.Fatal("assessment failure did not publish PAC withdrawal")
	}
}

func TestGatewayDeadlineTimerReassessesAndWithdrawsHTTPS(t *testing.T) {
	source, upstreams, path := createTrafficConfig(t, "https://api.example.test\n")
	engine, err := newRuntime(path, source, upstreams)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(engine)
	assessment := testUserCASnapshot(t, time.Now().Add(75*time.Millisecond), false)
	if err := engine.SetInitialHTTPSReadiness(assessment, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.DesiredStates():
	default:
	}
	var inspectCalls atomic.Int32
	ca := &fakeUserCA{
		inspect: func(context.Context) (userca.Assessment, error) {
			if inspectCalls.Add(1) == 1 {
				return assessment, nil
			}
			return userca.Assessment{}, nil
		},
	}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRuntime{engine: engine, phase: runtimePhaseRunning}
	lifecycle.runtime = active
	lifecycle.scheduleHTTPSDeadline(active, assessment)

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		state := engine.snapshot()
		if state.HTTPSInterception == HTTPSInterceptionInactive && inspectCalls.Load() >= 2 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("Gateway deadline timer did not deactivate HTTPS")
		case <-time.After(10 * time.Millisecond):
		}
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
			return userca.NewInstallResult(userca.Assessment{}, true), nil
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
	if result.Kind() != StartResultStartAlreadyMutating {
		t.Fatalf("transient start result = %s", result.Kind())
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
	source, snapshot, path := createTrafficConfig(t, "https://api.example.test\n")
	engine, err := newRuntime(path, source, snapshot)
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
		assessment: installed,
		uninstall: func(context.Context) (userca.UninstallResult, error) {
			inactiveDuringUninstall = engine.snapshot().HTTPSInterception == HTTPSInterceptionInactive
			return userca.NewUninstallResult(userca.Assessment{}, true), nil
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
	started, ok := result.(Started)
	if !ok || started.Guidance.HTTPSReadiness != HTTPSReadinessNotReady ||
		!hasHTTPSWarning(started.Guidance.HTTPSWarnings, HTTPSWarningUnmetIntent) {
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
	ca := &fakeUserCA{assessment: installed}
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
	started, ok := result.(Started)
	if !ok || started.Guidance.HTTPSReadiness != HTTPSReadinessReady {
		t.Fatalf("start result = %#v", result)
	}
	if ca.inspectCalls <= inspectionsAtConstruction {
		t.Fatal("start reused construction-time UserCA inspection")
	}
}

type fakeUserCA struct {
	mu              sync.Mutex
	assessment      userca.Assessment
	inspectErr      error
	inspect         func(context.Context) (userca.Assessment, error)
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

func (f *fakeUserCA) Inspect(ctx context.Context) (userca.Assessment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inspectCalls++
	if f.inspect != nil {
		return f.inspect(ctx)
	}
	return f.assessment, f.inspectErr
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

func (emptyTestUserCA) Inspect(context.Context) (userca.Assessment, error) {
	return userca.Assessment{}, nil
}
func (emptyTestUserCA) Install(context.Context) (userca.InstallResult, error) {
	return userca.InstallResult{}, nil
}
func (emptyTestUserCA) Uninstall(context.Context) (userca.UninstallResult, error) {
	return userca.UninstallResult{}, nil
}

type lifecycleTestSystemSettings struct {
	services      []managedpac.Service
	applied       int
	installResult *managedpac.InstallResult
	installErr    error
	stateErr      error
	clearErr      error
	cleared       int
}

func (f *lifecycleTestSystemSettings) Inspect(context.Context) (managedpac.Snapshot, error) {
	if f.stateErr != nil {
		return managedpac.Snapshot{}, f.stateErr
	}
	return managedpac.NewSnapshot(f.services), nil
}

func (f *lifecycleTestSystemSettings) InstallDesired(_ context.Context, services []string, desired managedpac.DesiredState) (managedpac.InstallResult, error) {
	f.applied++
	if f.installResult != nil {
		return *f.installResult, f.installErr
	}
	return managedpac.NewInstallResult(
		managedpac.NewRuntimeState(sortedUniqueServiceNames(services), managedpac.PACURL(desired.PACListen, 1)),
		sortedUniqueServiceNames(services),
		nil,
	), f.installErr
}

func (*lifecycleTestSystemSettings) PublishDesiredState(managedpac.DesiredState) {}

func (f *lifecycleTestSystemSettings) Uninstall(context.Context) error {
	f.cleared++
	if f.clearErr != nil {
		return f.clearErr
	}
	return nil
}

func executeAcceptedStart(lifecycle *lifecycle) (StartResult, error) {
	first, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	consentResult, ok := first.(StartConsentRequired)
	if err != nil || !ok {
		return first, err
	}
	return lifecycle.ExecuteStart(context.Background(), StartRequest{ManagedPACConsent: &ManagedPACConsentInput{
		ServiceNames: consentResult.Consent.ProposedServices,
		Fingerprint:  consentResult.Consent.Fingerprint,
	}})
}

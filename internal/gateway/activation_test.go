package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/systempac"
)

func TestExecuteStartComposesTrafficAndDeliversPAC(t *testing.T) {
	home := t.TempDir()
	globalPath := filepath.Join(home, "upstreams.txt")
	if err := os.WriteFile(globalPath, []byte("api.example.test\nhttp://plain.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{
		services:     []systemPACTestService{{ServiceName: "Wi-Fi", Ownership: systempac.OwnershipEmpty}},
		routingReady: true,
	}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.globalUpstreamListPath = globalPath

	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{WorkingDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = lifecycle.Stop(context.Background()) })
	started, ok := result.(Started)
	if !ok {
		t.Fatalf("start = %#v", result)
	}
	if settings.applied != 1 || started.Guidance.Traffic.HTTPCORS != TrafficFeatureActive {
		t.Fatalf("PAC installs = %d, traffic = %#v", settings.applied, started.Guidance.Traffic)
	}
	if started.Guidance.Traffic.HTTPSCORS != TrafficFeatureInactive ||
		started.Guidance.Traffic.HTTPSFacade != TrafficFeatureInactive {
		t.Fatalf("unexpected HTTPS outcomes = %#v", started.Guidance.Traffic)
	}

	settings.routingReady = false
	status, err := lifecycle.Status(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if status.Runtime == nil || status.Runtime.Traffic.HTTPCORS != TrafficFeatureBlocked {
		t.Fatalf("status did not use fresh System PAC observation: %#v", status.Runtime)
	}
}

func TestExecuteStartKeepsRuntimeActiveWhenSystemPACDeliveryFails(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(globalPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{setErr: systempac.MutationError{ServiceName: "Wi-Fi", Cause: errors.New("denied")}}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.globalUpstreamListPath = globalPath
	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{WorkingDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	started, ok := result.(Started)
	if !ok || !lifecycle.RuntimeActive() || len(started.Guidance.SystemPAC.Issues) != 1 {
		t.Fatalf("result/runtime = %#v / %t", result, lifecycle.RuntimeActive())
	}
	settings.setErr = nil
	status, err := lifecycle.Status(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if status.Runtime == nil || len(status.SystemPAC.Issues) != 0 || status.Runtime.LatestSystemPACDelivery == nil || len(status.Runtime.LatestSystemPACDelivery.Issues) != 1 {
		t.Fatalf("current and historical reports = %#v", status.Runtime)
	}
	_, _ = lifecycle.Stop(context.Background())
}

func TestRepeatedStartMakesDistinctDeliveryWithoutReplacingRuntime(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(globalPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.globalUpstreamListPath = globalPath
	if _, err := lifecycle.ExecuteStart(context.Background(), StartRequest{WorkingDirectory: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	before := lifecycle.runtime.engine.snapshot()
	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{WorkingDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	after := lifecycle.runtime.engine.snapshot()
	if _, ok := result.(AlreadyRunning); !ok || settings.applied != 2 {
		t.Fatalf("result/applied = %#v / %d", result, settings.applied)
	}
	if before.ProxyListen != after.ProxyListen || before.PACListen != after.PACListen {
		t.Fatalf("runtime endpoints changed: %#v -> %#v", before, after)
	}
	_, _ = lifecycle.Stop(context.Background())
}

func TestExecuteStartLoadsGlobalAndDirectoryUpstreamLists(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(globalPath, []byte("global.example.test\nshared.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workingDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(workingDirectory, "upstreams.txt"), []byte("shared.example.test\ndirectory.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{services: []systemPACTestService{{ServiceName: "Wi-Fi", Ownership: systempac.OwnershipEmpty}}}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.globalUpstreamListPath = globalPath
	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{WorkingDirectory: workingDirectory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = lifecycle.Stop(context.Background()) })
	if _, ok := result.(Started); !ok {
		t.Fatalf("start = %#v", result)
	}
	status, err := lifecycle.Status(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if status.Runtime == nil || status.Runtime.UpstreamCount != 3 || len(status.Runtime.UpstreamLists) != 2 {
		t.Fatalf("runtime = %#v", status.Runtime)
	}
}

func TestStartReportsBlockedHTTPSAndAssessmentIssue(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(globalPath, []byte("https://secure.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{
		services:     []systemPACTestService{{ServiceName: "Wi-Fi", Ownership: systempac.OwnershipEmpty}},
		routingReady: true,
	}
	ca := &fakeUserCA{inspectErr: errors.New("trust store unavailable")}
	lifecycle, err := newLifecycle(settings, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.globalUpstreamListPath = globalPath
	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{WorkingDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = lifecycle.Stop(context.Background()) })
	started := result.(Started)
	if started.Guidance.Traffic.HTTPSCORS != TrafficFeatureBlocked || started.Guidance.UserCAIssue == nil {
		t.Fatalf("guidance = %#v", started.Guidance)
	}
}

func TestInstallSwitchesActiveRuntimeToMatchingUserCAProjection(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileContents("https://secure.example.test\nhttp://plain.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	drainRuntimeDeliveries(t, runtime)
	installed := testUserCAState(t, time.Now().Add(24*time.Hour), false)
	ca := &fakeUserCA{installState: installed}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRuntime{engine: runtime, ctx: context.Background(), phase: runtimePhaseRunning}
	lifecycle.runtime = active

	result, err := lifecycle.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if result.Kind != InstallResultInstalled || !state.ServedHTTPSCORS || !state.ServedHTTPSFacade || !state.UserCAIdentityMatches {
		t.Fatalf("result = %#v, traffic = %#v", result, state)
	}
}

func TestInstallWithdrawsHTTPSBeforeUserCAMutation(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileContents("https://secure.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	drainRuntimeDeliveries(t, runtime)
	current := testUserCAState(t, time.Now().Add(24*time.Hour), false)
	runtime.AdoptUserCA(current, nil)
	withdrawn := false
	ca := &fakeUserCA{install: func(context.Context) (userCAState, error) {
		withdrawn = !runtime.snapshot().ServedHTTPSCORS
		return current, nil
	}}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: runtime, ctx: context.Background(), phase: runtimePhaseRunning}
	if _, err := lifecycle.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !withdrawn {
		t.Fatal("UserCA mutation began before HTTPS traffic was withdrawn")
	}
}

func TestUserCADeadlineInvalidatesServedHTTPSBeforeReassessment(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileContents("https://secure.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	drainRuntimeDeliveries(t, runtime)
	current := testUserCAState(t, time.Now().Add(time.Hour), false)
	runtime.AdoptUserCA(current, nil)
	ca := &fakeUserCA{}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRuntime{engine: runtime, ctx: context.Background(), phase: runtimePhaseRunning}
	lifecycle.runtime = active
	lifecycle.userCAState = current

	lifecycle.handleUserCADeadline(active, runtime.snapshot().UserCARevision)
	if runtime.snapshot().ServedHTTPSCORS {
		t.Fatal("expired UserCA left HTTPS routes served")
	}
}

func TestInstallUsesOnlyUserCAAndDoesNotCreateUpstreamList(t *testing.T) {
	ca := &fakeUserCA{installState: testUserCAState(t, time.Now().Add(24*time.Hour), false)}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(t.TempDir(), "upstreams.txt")
	lifecycle.globalUpstreamListPath = globalPath
	if _, err := lifecycle.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Fatalf("install touched Upstream List: %v", err)
	}
}

func fileContents(value string) fileobservation.Contents { return fileobservation.Contents(value) }

func drainRuntimeDeliveries(t *testing.T, runtime *trafficRuntime) {
	t.Helper()
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-runtime.DeliveryRequests():
			case <-stop:
				return
			}
		}
	}()
}

type fakeUserCA struct {
	mu             sync.Mutex
	state          userCAState
	inspectErr     error
	inspect        func(context.Context) (userCAState, error)
	installState   userCAState
	installErr     error
	uninstallErr   error
	install        func(context.Context) (userCAState, error)
	uninstall      func(context.Context) error
	inspectCalls   int
	installCalls   int
	uninstallCalls int
}

func (f *fakeUserCA) Inspect(ctx context.Context) (userCAState, error) {
	f.mu.Lock()
	f.inspectCalls++
	operation := f.inspect
	state, err := f.state, f.inspectErr
	f.mu.Unlock()
	if operation != nil {
		return operation(ctx)
	}
	return state, err
}

func (f *fakeUserCA) Install(ctx context.Context) (userCAState, error) {
	f.mu.Lock()
	f.installCalls++
	operation := f.install
	state, err := f.installState, f.installErr
	f.mu.Unlock()
	if operation != nil {
		return operation(ctx)
	}
	return state, err
}

func (f *fakeUserCA) Uninstall(ctx context.Context) error {
	f.mu.Lock()
	f.uninstallCalls++
	operation := f.uninstall
	err := f.uninstallErr
	f.mu.Unlock()
	if operation != nil {
		return operation(ctx)
	}
	return err
}

type emptyTestUserCA struct{}

func (emptyTestUserCA) Inspect(context.Context) (userCAState, error) { return userCAState{}, nil }
func (emptyTestUserCA) Install(context.Context) (userCAState, error) { return userCAState{}, nil }
func (emptyTestUserCA) Uninstall(context.Context) error              { return nil }

type lifecycleTestSystemSettings struct {
	services       []systemPACTestService
	applied        int
	setResult      *systempac.State
	setErr         error
	stateErr       error
	clearErr       error
	cleared        int
	cleanupCalls   int
	uninstallCalls int
	cleanupResult  []systempac.ServiceState
	routingReady   bool
}

type systemPACTestService struct {
	ServiceName      string
	URL              string
	Enabled          bool
	Ownership        systempac.Ownership
	ObservationIssue string
}

func (f *lifecycleTestSystemSettings) Deliver(_ context.Context, _ string) (systempac.State, error) {
	f.applied++
	if f.setResult != nil {
		return *f.setResult, f.setErr
	}
	return f.state(), f.setErr
}

func (f *lifecycleTestSystemSettings) Observe(context.Context, string) (systempac.State, error) {
	return f.state(), f.stateErr
}

func (f *lifecycleTestSystemSettings) state() systempac.State {
	state := systempac.State{RoutesCurrentEndpoint: f.routingReady}
	for _, service := range f.services {
		state.Services = append(state.Services, systempac.ServiceState{Name: service.ServiceName, URL: service.URL, Enabled: service.Enabled, Ownership: service.Ownership})
	}
	return state
}

func (f *lifecycleTestSystemSettings) Cleanup(context.Context) ([]systempac.ServiceState, error) {
	f.cleared++
	f.cleanupCalls++
	return f.cleanupResult, f.clearErr
}

func executeAcceptedStart(t *testing.T, lifecycle *lifecycle) (StartResult, error) {
	t.Helper()
	return lifecycle.ExecuteStart(context.Background(), StartRequest{WorkingDirectory: t.TempDir()})
}

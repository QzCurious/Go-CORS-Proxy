package managedpac

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

type fakeSettings struct {
	mu           sync.Mutex
	states       []serviceSnapshot
	snapshotErr  error
	applyErrors  map[string]error
	applyStarted chan string
	applyRelease chan struct{}
	ignoreCancel bool
	writes       []string
	clearErr     error
}

func (f *fakeSettings) Snapshot(context.Context) ([]serviceSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	return append([]serviceSnapshot(nil), f.states...), nil
}

func (f *fakeSettings) Apply(ctx context.Context, pacURL string, serviceNames []string) (applyResult, error) {
	serviceName := serviceNames[0]
	if f.applyStarted != nil {
		select {
		case f.applyStarted <- pacURL:
		default:
		}
	}
	if f.applyRelease != nil {
		if f.ignoreCancel {
			<-f.applyRelease
		} else {
			select {
			case <-f.applyRelease:
			case <-ctx.Done():
				return applyResult{}, ctx.Err()
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.applyErrors[serviceName]; err != nil {
		return applyResult{}, err
	}
	for index := range f.states {
		if f.states[index].ServiceName == serviceName {
			f.states[index].PACURL = pacURL
			f.states[index].Enabled = true
			f.writes = append(f.writes, serviceName+"="+pacURL)
			return applyResult{AppliedServices: []string{serviceName}}, nil
		}
	}
	return applyResult{}, nil
}

func (f *fakeSettings) ClearOwned(_ context.Context, serviceNames []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.clearErr != nil {
		return f.clearErr
	}
	selected := stringSet(serviceNames)
	for index := range f.states {
		state := &f.states[index]
		if _, ok := selected[state.ServiceName]; ok && isOwnedURL(state.PACURL) {
			state.PACURL = ""
			state.Enabled = false
		}
	}
	return nil
}

func TestInspectClassifiesOnlyEmptyAndOwnedServicesAsManageable(t *testing.T) {
	module := openWithSettings(&fakeSettings{states: []serviceSnapshot{
		{ServiceName: "Wi-Fi", PACURL: "http://corp.example/proxy.pac", Enabled: true},
		{ServiceName: "USB", PACURL: "", Enabled: false},
		{ServiceName: "Ethernet", PACURL: "http://127.0.0.1:49152/seamless-cors.pac?v=1", Enabled: false},
	}})

	snapshot, err := module.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := snapshot.ManageableServices(), []string{"Ethernet", "USB"}; !slices.Equal(got, want) {
		t.Fatalf("manageable services = %v, want %v", got, want)
	}
	services := snapshot.Services()
	if services[2].Name != "Wi-Fi" || services[2].Ownership != OwnershipForeign {
		t.Fatalf("services not sorted or classified: %#v", services)
	}
}

func TestInstallKeepsFixedSetAndUpdatesEligibleSubset(t *testing.T) {
	settings := &fakeSettings{
		states: []serviceSnapshot{
			{ServiceName: "Ethernet"},
			{ServiceName: "Wi-Fi", PACURL: "http://corp.example/proxy.pac", Enabled: true},
			{ServiceName: "New VPN"},
		},
	}
	module := openWithSettings(settings)

	result, err := module.Install(context.Background(), []string{"Missing", "Wi-Fi", "Ethernet"}, "http://127.0.0.1/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.State().ServiceNames(), []string{"Ethernet", "Missing", "Wi-Fi"}; !slices.Equal(got, want) {
		t.Fatalf("fixed set = %v, want %v", got, want)
	}
	if got, want := result.InstalledServices(), []string{"Ethernet"}; !slices.Equal(got, want) {
		t.Fatalf("installed = %v, want %v", got, want)
	}
	warnings := result.Warnings()
	if len(warnings) != 1 || warnings[0].Kind != WarningDrift || warnings[0].ServiceName != "Wi-Fi" {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(settings.writes) != 1 || settings.writes[0][:8] != "Ethernet" {
		t.Fatalf("writes = %v", settings.writes)
	}
}

func TestInstallFailsAfterReachingNoSelectedService(t *testing.T) {
	module := openWithSettings(&fakeSettings{states: []serviceSnapshot{{
		ServiceName: "Wi-Fi", PACURL: "http://corp.example/proxy.pac", Enabled: true,
	}}})

	result, err := module.Install(context.Background(), []string{"Wi-Fi", "Missing"}, "http://127.0.0.1/seamless-cors.pac?v=1")
	if !errors.Is(err, errNoServicesInstalled) {
		t.Fatalf("install error = %v", err)
	}
	if len(result.Warnings()) != 1 || result.Warnings()[0].Kind != WarningDrift {
		t.Fatalf("warnings = %#v", result.Warnings())
	}
}

func TestReconcilePreservesForeignAndIgnoresAbsentAndUnselectedServices(t *testing.T) {
	settings := &fakeSettings{states: []serviceSnapshot{
		{ServiceName: "Ethernet", PACURL: "http://localhost:8000/seamless-cors.pac?v=1", Enabled: false},
		{ServiceName: "Wi-Fi"},
		{ServiceName: "New VPN"},
	}}
	module := openWithSettings(settings)
	installed, err := module.Install(context.Background(), []string{"Ethernet", "Wi-Fi", "Missing"}, "http://127.0.0.1/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}
	settings.mu.Lock()
	settings.states[1] = serviceSnapshot{ServiceName: "Wi-Fi", PACURL: "http://corp.example/proxy.pac", Enabled: true}
	settings.writes = nil
	settings.mu.Unlock()

	done := make(chan ReconcileResult, 1)
	module.RequestReconcile(installed.State(), "http://127.0.0.1/seamless-cors.pac?v=2", func(result ReconcileResult) { done <- result })
	result := receive(t, done)

	if len(result.Warnings()) != 1 || result.Warnings()[0].ServiceName != "Wi-Fi" || result.Warnings()[0].Kind != WarningDrift {
		t.Fatalf("warnings = %#v", result.Warnings())
	}
	settings.mu.Lock()
	writes := append([]string(nil), settings.writes...)
	states := append([]serviceSnapshot(nil), settings.states...)
	settings.mu.Unlock()
	if len(writes) != 1 || writes[0] != "Ethernet=http://127.0.0.1/seamless-cors.pac?v=2" {
		t.Fatalf("writes = %v", writes)
	}
	if states[1].PACURL != "http://corp.example/proxy.pac" || states[2].PACURL != "" {
		t.Fatalf("foreign or unselected state changed: %#v", states)
	}
}

func TestNewReconcileRequestPreemptsOlderCompletion(t *testing.T) {
	settings := &fakeSettings{
		states: []serviceSnapshot{{ServiceName: "Wi-Fi"}},
	}
	module := openWithSettings(settings)
	installed, err := module.Install(context.Background(), []string{"Wi-Fi"}, "http://127.0.0.1/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}
	settings.applyStarted = make(chan string, 4)
	settings.applyRelease = make(chan struct{})
	firstDone := make(chan ReconcileResult, 1)
	latestDone := make(chan ReconcileResult, 1)
	module.RequestReconcile(installed.State(), "http://127.0.0.1/seamless-cors.pac?v=2", func(result ReconcileResult) { firstDone <- result })
	<-settings.applyStarted
	module.RequestReconcile(installed.State(), "http://127.0.0.1/seamless-cors.pac?v=3", func(result ReconcileResult) { latestDone <- result })
	close(settings.applyRelease)
	receive(t, latestDone)
	select {
	case <-firstDone:
		t.Fatal("superseded reconciliation published a completion")
	default:
	}
	settings.mu.Lock()
	writes := append([]string(nil), settings.writes...)
	settings.mu.Unlock()
	if got := writes[len(writes)-1]; got != "Wi-Fi=http://127.0.0.1/seamless-cors.pac?v=3" {
		t.Fatalf("latest write = %q", got)
	}
}

func TestReconcileInspectionFailureReturnsErrorWithoutFabricatingWarning(t *testing.T) {
	settings := &fakeSettings{states: []serviceSnapshot{{ServiceName: "Wi-Fi"}}}
	module := openWithSettings(settings)
	installed, err := module.Install(context.Background(), []string{"Wi-Fi"}, "http://127.0.0.1/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}
	settings.mu.Lock()
	settings.snapshotErr = errors.New("inspection denied")
	settings.mu.Unlock()

	done := make(chan ReconcileResult, 1)
	module.RequestReconcile(installed.State(), "http://127.0.0.1/seamless-cors.pac?v=2", func(result ReconcileResult) { done <- result })
	result := receive(t, done)

	if result.Err() == nil || len(result.Warnings()) != 0 {
		t.Fatalf("reconcile result = error %v, warnings %#v", result.Err(), result.Warnings())
	}
}

func TestUninstallRemovesDisabledAndChangedOwnedStateAndRejectsLateReconcile(t *testing.T) {
	settings := &fakeSettings{states: []serviceSnapshot{
		{ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1/seamless-cors.pac?v=1", Enabled: false},
		{ServiceName: "Ethernet", PACURL: "http://localhost:9000/seamless-cors.pac?v=9", Enabled: true},
		{ServiceName: "VPN", PACURL: "http://corp.example/proxy.pac", Enabled: true},
	}}
	module := openWithSettings(settings)
	module.mu.Lock()
	module.accepting = true
	module.mu.Unlock()
	state := RuntimeState{serviceNames: []string{"Wi-Fi", "Ethernet"}, pacURL: "old"}

	if err := module.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed := make(chan ReconcileResult, 1)
	module.RequestReconcile(state, "http://127.0.0.1/seamless-cors.pac?v=10", func(result ReconcileResult) { completed <- result })
	time.Sleep(20 * time.Millisecond)
	select {
	case <-completed:
		t.Fatal("late reconciliation completed after uninstall")
	default:
	}
	settings.mu.Lock()
	states := append([]serviceSnapshot(nil), settings.states...)
	settings.mu.Unlock()
	if states[0].PACURL != "" || states[1].PACURL != "" || states[2].PACURL != "http://corp.example/proxy.pac" {
		t.Fatalf("states after uninstall = %#v", states)
	}
}

func TestUninstallWaitsForWriterThenPreventsEveryLaterWrite(t *testing.T) {
	settings := &fakeSettings{states: []serviceSnapshot{{ServiceName: "Wi-Fi"}}}
	module := openWithSettings(settings)
	installed, err := module.Install(context.Background(), []string{"Wi-Fi"}, "http://127.0.0.1/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}
	settings.applyStarted = make(chan string, 2)
	settings.applyRelease = make(chan struct{})
	settings.ignoreCancel = true
	module.RequestReconcile(installed.State(), "http://127.0.0.1/seamless-cors.pac?v=2", nil)
	<-settings.applyStarted

	uninstallDone := make(chan error, 1)
	go func() { uninstallDone <- module.Uninstall(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for {
		module.mu.Lock()
		accepting := module.accepting
		module.mu.Unlock()
		if !accepting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("uninstall did not close reconciliation admission")
		}
		time.Sleep(time.Millisecond)
	}
	module.RequestReconcile(installed.State(), "http://127.0.0.1/seamless-cors.pac?v=3", nil)
	select {
	case err := <-uninstallDone:
		t.Fatalf("uninstall returned before writer quiesced: %v", err)
	default:
	}
	close(settings.applyRelease)
	if err := receive(t, uninstallDone); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)

	settings.mu.Lock()
	writes := append([]string(nil), settings.writes...)
	state := settings.states[0]
	settings.mu.Unlock()
	if len(writes) != 2 || writes[1] != "Wi-Fi=http://127.0.0.1/seamless-cors.pac?v=2" {
		t.Fatalf("writes = %v", writes)
	}
	if state.PACURL != "" || state.Enabled {
		t.Fatalf("state after uninstall = %#v", state)
	}
}

func TestUninstallClosureWinsOverOlderConcurrentInstall(t *testing.T) {
	settings := &fakeSettings{
		states:       []serviceSnapshot{{ServiceName: "Wi-Fi"}},
		applyStarted: make(chan string, 1),
		applyRelease: make(chan struct{}),
		ignoreCancel: true,
	}
	module := openWithSettings(settings)
	installDone := make(chan error, 1)
	go func() {
		_, err := module.Install(context.Background(), []string{"Wi-Fi"}, "http://127.0.0.1/seamless-cors.pac?v=1")
		installDone <- err
	}()
	<-settings.applyStarted

	uninstallDone := make(chan error, 1)
	go func() { uninstallDone <- module.Uninstall(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for {
		module.mu.Lock()
		generation := module.generation
		module.mu.Unlock()
		if generation >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("uninstall did not close reconciliation admission")
		}
		time.Sleep(time.Millisecond)
	}
	close(settings.applyRelease)
	if err := receive(t, installDone); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, uninstallDone); err != nil {
		t.Fatal(err)
	}

	completed := make(chan ReconcileResult, 1)
	module.RequestReconcile(RuntimeState{serviceNames: []string{"Wi-Fi"}}, "http://127.0.0.1/seamless-cors.pac?v=2", func(result ReconcileResult) {
		completed <- result
	})
	select {
	case <-completed:
		t.Fatal("reconciliation was admitted after uninstall closed a concurrent install")
	case <-time.After(20 * time.Millisecond):
	}
	settings.mu.Lock()
	state := settings.states[0]
	settings.mu.Unlock()
	if state.PACURL != "" || state.Enabled {
		t.Fatalf("state after uninstall = %#v", state)
	}
}

func TestOwnedURLMatchesOnlyLoopbackHTTPPACFilename(t *testing.T) {
	tests := map[string]bool{
		"http://127.0.0.1:8079/seamless-cors.pac":        true,
		"http://127.0.0.1:8079/seamless-cors.pac?v=2":    true,
		"http://localhost:8079/nested/seamless-cors.pac": true,
		"http://[::1]:8079/seamless-cors.pac":            true,
		"http://127.0.0.1:8079/not-seamless-cors.pac":    false,
		"https://127.0.0.1:8079/seamless-cors.pac":       false,
		"http://proxy.example.test/seamless-cors.pac":    false,
	}
	for raw, want := range tests {
		if got := isOwnedURL(raw); got != want {
			t.Fatalf("isOwnedURL(%q) = %t, want %t", raw, got, want)
		}
	}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
		var zero T
		return zero
	}
}

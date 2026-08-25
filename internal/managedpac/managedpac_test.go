package managedpac

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/pacsettings"
	"github.com/QzCurious/seamless-cors/internal/pacrouting"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

type fakeSettings struct {
	mu            sync.Mutex
	states        []pacsettings.Setting
	snapshotErr   error
	applyErrors   map[string]error
	beforeSet     func(*fakeSettings)
	beforeDisable func(*fakeSettings)
	writes        []string
	clearErr      error
	disableErrors map[string]error
}

func (f *fakeSettings) List(context.Context) ([]pacsettings.Setting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	return append([]pacsettings.Setting(nil), f.states...), nil
}

func (f *fakeSettings) SetURL(ctx context.Context, observed pacsettings.Setting, pacURL string) (pacsettings.MutationResult, error) {
	serviceName := observed.ServiceName
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforeSet != nil {
		beforeSet := f.beforeSet
		f.beforeSet = nil
		beforeSet(f)
	}
	if err := ctx.Err(); err != nil {
		return pacsettings.MutationResult{}, err
	}
	if err := f.applyErrors[serviceName]; err != nil {
		return pacsettings.MutationResult{}, err
	}
	for index := range f.states {
		if f.states[index].ServiceName == serviceName {
			if f.states[index] != observed {
				current := f.states[index]
				return pacsettings.MutationResult{Current: &current}, nil
			}
			f.states[index].URL = pacURL
			f.states[index].Enabled = true
			f.writes = append(f.writes, serviceName+"="+pacURL)
			return pacsettings.MutationResult{Applied: true}, nil
		}
	}
	return pacsettings.MutationResult{}, nil
}

func (f *fakeSettings) Disable(_ context.Context, observed pacsettings.Setting) (pacsettings.MutationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforeDisable != nil {
		beforeDisable := f.beforeDisable
		f.beforeDisable = nil
		beforeDisable(f)
	}
	if f.clearErr != nil {
		return pacsettings.MutationResult{}, f.clearErr
	}
	if err := f.disableErrors[observed.ServiceName]; err != nil {
		return pacsettings.MutationResult{}, err
	}
	for index := range f.states {
		state := &f.states[index]
		if state.ServiceName != observed.ServiceName {
			continue
		}
		if *state != observed {
			current := *state
			return pacsettings.MutationResult{Current: &current}, nil
		}
		if state.Enabled {
			state.Enabled = false
		}
		return pacsettings.MutationResult{Applied: true}, nil
	}
	return pacsettings.MutationResult{}, nil
}

func TestInspectClassifiesOnlyEmptyAndOwnedServicesAsManageable(t *testing.T) {
	module := openWithSettings(&fakeSettings{states: []pacsettings.Setting{
		{ServiceName: "Wi-Fi", URL: "http://corp.example/proxy.pac", Enabled: true},
		{ServiceName: "USB", URL: "", Enabled: false},
		{ServiceName: "Ethernet", URL: "http://127.0.0.1:49152/seamless-cors.pac?v=1", Enabled: false},
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

func TestSnapshotDistinguishesOwnedURLFromActiveOwnedState(t *testing.T) {
	snapshot := NewSnapshot([]Service{
		{Name: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: false, Ownership: OwnershipOwned},
	})

	if !snapshot.HasOwnedState() {
		t.Fatal("disabled retained URL should remain classified as owned")
	}
	if snapshot.HasActiveOwnedState() {
		t.Fatal("disabled retained URL must not require cleanup")
	}
}

func TestCleanupActiveStateRejectsOpenReconciliation(t *testing.T) {
	settings := &fakeSettings{states: []pacsettings.Setting{{ServiceName: "Wi-Fi"}}}
	module := openWithSettings(settings)
	if _, err := module.InstallProjection(context.Background(), []string{"Wi-Fi"}, "127.0.0.1:8081", "DIRECT"); err != nil {
		t.Fatal(err)
	}

	err := module.CleanupActiveState(context.Background())
	var activeErr CleanupWhileReconciliationActiveError
	if !errors.As(err, &activeErr) {
		t.Fatalf("cleanup error = %v, want CleanupWhileReconciliationActiveError", err)
	}
}

func TestCleanupActiveStateContinuesAndReportsPerServiceFailures(t *testing.T) {
	settings := &fakeSettings{
		states: []pacsettings.Setting{
			{ServiceName: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true},
			{ServiceName: "USB", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true},
		},
		disableErrors: map[string]error{"Wi-Fi": errors.New("permission denied")},
	}
	module := openWithSettings(settings)

	err := module.CleanupActiveState(context.Background())
	var cleanupErr ActiveStateCleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("cleanup error = %v, want ActiveStateCleanupError", err)
	}
	if len(cleanupErr.ServiceFailures) != 1 || cleanupErr.ServiceFailures[0].ServiceName != "Wi-Fi" {
		t.Fatalf("service failures = %#v", cleanupErr.ServiceFailures)
	}
	if !slices.Equal(cleanupErr.RemainingServices, []string{"Wi-Fi"}) {
		t.Fatalf("remaining services = %v, want Wi-Fi", cleanupErr.RemainingServices)
	}
	settings.mu.Lock()
	defer settings.mu.Unlock()
	if settings.states[1].Enabled {
		t.Fatal("USB cleanup was not attempted after Wi-Fi failed")
	}
}

func TestCleanupPreservesForeignStateThatAppearsAfterInspection(t *testing.T) {
	settings := &fakeSettings{
		states: []pacsettings.Setting{{ServiceName: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true}},
		beforeDisable: func(settings *fakeSettings) {
			settings.states[0].URL = "http://corp.example/proxy.pac"
		},
	}
	module := openWithSettings(settings)

	if err := module.CleanupActiveState(context.Background()); err != nil {
		t.Fatal(err)
	}
	settings.mu.Lock()
	defer settings.mu.Unlock()
	if !settings.states[0].Enabled || settings.states[0].URL != "http://corp.example/proxy.pac" {
		t.Fatalf("foreign setting was changed: %#v", settings.states[0])
	}
}

func TestCleanupReportsOwnedStateThatChangesAfterInspection(t *testing.T) {
	settings := &fakeSettings{
		states: []pacsettings.Setting{{ServiceName: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac?v=1", Enabled: true}},
		beforeDisable: func(settings *fakeSettings) {
			settings.states[0].URL = "http://127.0.0.1/seamless-cors.pac?v=2"
		},
	}
	module := openWithSettings(settings)

	err := module.CleanupActiveState(context.Background())
	var cleanupErr ActiveStateCleanupError
	if !errors.As(err, &cleanupErr) || !slices.Equal(cleanupErr.RemainingServices, []string{"Wi-Fi"}) {
		t.Fatalf("cleanup error = %#v", err)
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

func TestProjectionChangeAdvancesGenerationBeforePublication(t *testing.T) {
	first := mustDesiredList(t, "api.example.test\n")
	second := mustDesiredList(t, "other.example.test\n")
	settings := &fakeSettings{states: []pacsettings.Setting{{ServiceName: "Wi-Fi"}}}
	module := openWithSettings(settings)
	if _, err := module.InstallProjection(context.Background(), []string{"Wi-Fi"}, "127.0.0.1:8081", pacrouting.Project(first, false, "127.0.0.1:8080")); err != nil {
		t.Fatal(err)
	}
	module.PublishProjection(pacrouting.Project(second, false, "127.0.0.1:8080"))
	waitForWrite(t, settings, "v=2")
	if got := publicationGeneration(module); got != 2 {
		t.Fatalf("publication generation = %d, want 2", got)
	}
}

func TestPublicationPreservesForeignStateThatAppearsAfterInspection(t *testing.T) {
	settings := &fakeSettings{
		states: []pacsettings.Setting{{ServiceName: "Wi-Fi"}},
		beforeSet: func(settings *fakeSettings) {
			settings.states[0] = pacsettings.Setting{ServiceName: "Wi-Fi", URL: "http://corp.example/proxy.pac", Enabled: true}
		},
	}
	module := openWithSettings(settings)

	result, err := module.InstallProjection(context.Background(), []string{"Wi-Fi"}, "127.0.0.1:8081", "DIRECT")
	if err != nil {
		t.Fatal(err)
	}
	if warnings := result.Warnings(); len(warnings) != 1 || warnings[0].Kind != WarningDrift {
		t.Fatalf("warnings = %#v", warnings)
	}
	settings.mu.Lock()
	defer settings.mu.Unlock()
	if len(settings.writes) != 0 || settings.states[0].URL != "http://corp.example/proxy.pac" {
		t.Fatalf("foreign setting was overwritten: state=%#v writes=%v", settings.states[0], settings.writes)
	}
}

func TestChangedManageablePublicationIsRetriedWithoutReturningGatewayError(t *testing.T) {
	settings := &fakeSettings{
		states: []pacsettings.Setting{{ServiceName: "Wi-Fi"}},
		beforeSet: func(settings *fakeSettings) {
			settings.states[0].URL = "http://127.0.0.1:8081/seamless-cors.pac?v=0"
		},
	}
	module := openWithSettings(settings)
	result, err := module.InstallProjection(context.Background(), []string{"Wi-Fi"}, "127.0.0.1:8081", "DIRECT")
	if err != nil {
		t.Fatal(err)
	}
	if warnings := result.Warnings(); len(warnings) != 1 || warnings[0].Kind != WarningUpdateFailed {
		t.Fatalf("warnings = %#v", warnings)
	}
	waitForWrite(t, settings, "seamless-cors.pac?v=2")
}

func TestPartialProjectionPublicationFailureIsRetried(t *testing.T) {
	first := mustDesiredList(t, "api.example.test\n")
	second := mustDesiredList(t, "other.example.test\n")
	settings := &fakeSettings{states: []pacsettings.Setting{
		{ServiceName: "Ethernet"},
		{ServiceName: "Wi-Fi"},
	}}
	module := openWithSettings(settings)
	if _, err := module.InstallProjection(context.Background(), []string{"Ethernet", "Wi-Fi"}, "127.0.0.1:8081", pacrouting.Project(first, false, "127.0.0.1:8080")); err != nil {
		t.Fatal(err)
	}

	settings.mu.Lock()
	settings.applyErrors = map[string]error{"Ethernet": errors.New("write denied")}
	settings.mu.Unlock()
	module.PublishProjection(pacrouting.Project(second, false, "127.0.0.1:8080"))
	waitForGeneration(t, module, 2)

	settings.mu.Lock()
	settings.applyErrors = nil
	settings.mu.Unlock()
	waitForWrite(t, settings, "Ethernet=http://127.0.0.1:8081/seamless-cors.pac?v=3")
}

func TestFailedProjectionPublicationConsumesGenerationAndRetriesLatestState(t *testing.T) {
	first := mustDesiredList(t, "api.example.test\n")
	second := mustDesiredList(t, "second.example.test\n")
	third := mustDesiredList(t, "third.example.test\n")
	settings := &fakeSettings{states: []pacsettings.Setting{{ServiceName: "Wi-Fi"}}}
	module := openWithSettings(settings)
	if _, err := module.InstallProjection(context.Background(), []string{"Wi-Fi"}, "127.0.0.1:8081", pacrouting.Project(first, false, "127.0.0.1:8080")); err != nil {
		t.Fatal(err)
	}
	settings.mu.Lock()
	settings.applyErrors = map[string]error{"Wi-Fi": errors.New("write denied")}
	settings.mu.Unlock()
	module.PublishProjection(pacrouting.Project(second, false, "127.0.0.1:8080"))
	waitForGeneration(t, module, 2)

	settings.mu.Lock()
	settings.applyErrors = nil
	settings.mu.Unlock()
	module.PublishProjection(pacrouting.Project(third, false, "127.0.0.1:8080"))
	waitForGeneration(t, module, 3)
	waitForWrite(t, settings, "v=3")
	settings.mu.Lock()
	writes := append([]string(nil), settings.writes...)
	settings.mu.Unlock()
	if len(writes) < 2 {
		t.Fatalf("writes after retry = %v", writes)
	}
	if got := publicationGeneration(module); got < 3 {
		t.Fatalf("retry did not consume a new generation: %d", got)
	}
}

func TestReconciliationResultsReplaceDriftAndFailureWarningsOnRecovery(t *testing.T) {
	first := mustDesiredList(t, "api.example.test\n")
	second := mustDesiredList(t, "second.example.test\n")
	settings := &fakeSettings{states: []pacsettings.Setting{{ServiceName: "Wi-Fi"}}}
	module := openWithSettings(settings)
	install, err := module.InstallProjection(context.Background(), []string{"Wi-Fi"}, "127.0.0.1:8081", pacrouting.Project(first, false, "127.0.0.1:8080"))
	if err != nil {
		t.Fatal(err)
	}
	initialURL := install.State().PACURL()

	settings.mu.Lock()
	settings.states[0].URL = "http://corp.example/proxy.pac"
	settings.mu.Unlock()
	module.PublishProjection(pacrouting.Project(second, false, "127.0.0.1:8080"))
	drift := receiveReconciliation(t, module.ReconciliationResults())
	if warnings := drift.Warnings(); len(warnings) != 1 || warnings[0].Kind != WarningDrift {
		t.Fatalf("drift warnings = %#v", warnings)
	}
	if drift.State().PACURL() == initialURL {
		t.Fatalf("drift publication URL did not advance: %q", drift.State().PACURL())
	}

	settings.mu.Lock()
	settings.states[0].URL = initialURL
	settings.applyErrors = map[string]error{"Wi-Fi": errors.New("write denied")}
	settings.mu.Unlock()
	module.PublishProjection(pacrouting.Project(second, false, "127.0.0.1:8080"))
	failed := receiveReconciliation(t, module.ReconciliationResults())
	if warnings := failed.Warnings(); len(warnings) != 1 || warnings[0].Kind != WarningUpdateFailed {
		t.Fatalf("failure warnings = %#v", warnings)
	}
	failedURL := failed.State().PACURL()

	settings.mu.Lock()
	settings.applyErrors = nil
	settings.mu.Unlock()
	recovered := receiveReconciliation(t, module.ReconciliationResults())
	if warnings := recovered.Warnings(); len(warnings) != 0 {
		t.Fatalf("recovery warnings = %#v, want cleared", warnings)
	}
	if recovered.State().PACURL() == failedURL {
		t.Fatalf("retry did not publish a new generation: %q", recovered.State().PACURL())
	}
}

func receiveReconciliation(t *testing.T, results <-chan ReconciliationResult) ReconciliationResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Managed PAC reconciliation result")
		return ReconciliationResult{}
	}
}

func mustDesiredList(t *testing.T, contents string) upstreamlist.Projection {
	t.Helper()
	list, err := upstreamlist.Project([]byte(contents))
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func waitForWrite(t *testing.T, settings *fakeSettings, fragment string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		settings.mu.Lock()
		for _, write := range settings.writes {
			if strings.Contains(write, fragment) {
				settings.mu.Unlock()
				return
			}
		}
		settings.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for write %q", fragment)
}

func waitForGeneration(t *testing.T, module *ManagedPAC, want uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if publicationGeneration(module) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("publication generation did not reach %d", want)
}

func publicationGeneration(module *ManagedPAC) uint64 {
	module.mu.Lock()
	defer module.mu.Unlock()
	return module.publicationGeneration
}

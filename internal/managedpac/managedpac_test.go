package managedpac

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/pacrouting"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

type fakeSettings struct {
	mu            sync.Mutex
	states        []serviceSnapshot
	snapshotErr   error
	applyErrors   map[string]error
	applyStarted  chan string
	applyRelease  chan struct{}
	ignoreCancel  bool
	writes        []string
	clearErr      error
	disableErrors map[string]error
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

func (f *fakeSettings) DisableOwned(_ context.Context, serviceNames []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.clearErr != nil {
		return f.clearErr
	}
	if err := f.disableErrors[serviceNames[0]]; err != nil {
		return err
	}
	selected := stringSet(serviceNames)
	for index := range f.states {
		state := &f.states[index]
		if _, ok := selected[state.ServiceName]; ok && state.Enabled && IsOwnedURL(state.PACURL) {
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
	settings := &fakeSettings{states: []serviceSnapshot{{ServiceName: "Wi-Fi"}}}
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
		states: []serviceSnapshot{
			{ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true},
			{ServiceName: "USB", PACURL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true},
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
		if got := IsOwnedURL(raw); got != want {
			t.Fatalf("IsOwnedURL(%q) = %t, want %t", raw, got, want)
		}
	}
}

func TestProjectionInstallsCompletePAC(t *testing.T) {
	list := mustDesiredList(t, "api.example.test\n")
	settings := &fakeSettings{states: []serviceSnapshot{{ServiceName: "Wi-Fi"}}}
	module := openWithSettings(settings)
	projection := pacrouting.Project(list, false, "127.0.0.1:8080")
	_, err := module.InstallProjection(context.Background(), []string{"Wi-Fi"}, "127.0.0.1:8081", projection)
	if err != nil {
		t.Fatal(err)
	}
	if got := module.PublicationGeneration(); got != 1 {
		t.Fatalf("initial publication generation = %d, want 1", got)
	}
	settings.mu.Lock()
	defer settings.mu.Unlock()
	if !strings.Contains(settings.writes[0], "v=1") {
		t.Fatalf("initial publication URL = %q", settings.writes[0])
	}
}

func TestProjectionChangeAdvancesGenerationBeforePublication(t *testing.T) {
	first := mustDesiredList(t, "api.example.test\n")
	second := mustDesiredList(t, "other.example.test\n")
	settings := &fakeSettings{states: []serviceSnapshot{{ServiceName: "Wi-Fi"}}}
	module := openWithSettings(settings)
	if _, err := module.InstallProjection(context.Background(), []string{"Wi-Fi"}, "127.0.0.1:8081", pacrouting.Project(first, false, "127.0.0.1:8080")); err != nil {
		t.Fatal(err)
	}
	module.PublishProjection(pacrouting.Project(second, false, "127.0.0.1:8080"))
	waitForWrite(t, settings, "v=2")
	if got := module.PublicationGeneration(); got != 2 {
		t.Fatalf("publication generation = %d, want 2", got)
	}
}

func TestInitialProjectionPublicationFailureIsRetriedWithoutReturningGatewayError(t *testing.T) {
	list := mustDesiredList(t, "api.example.test\n")
	settings := &fakeSettings{
		states:      []serviceSnapshot{{ServiceName: "Wi-Fi"}},
		applyErrors: map[string]error{"Wi-Fi": errors.New("write denied")},
	}
	module := openWithSettings(settings)
	desired := pacrouting.Project(list, false, "127.0.0.1:8080")
	result, err := module.InstallProjection(context.Background(), []string{"Wi-Fi"}, "127.0.0.1:8081", desired)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.InstalledServices()) != 0 {
		t.Fatalf("initial installed services = %v, want none", result.InstalledServices())
	}

	waitForGeneration(t, module, 2)
	settings.mu.Lock()
	settings.applyErrors = nil
	settings.mu.Unlock()
	waitForWrite(t, settings, "seamless-cors.pac?v=")
}

func TestPartialProjectionPublicationFailureIsRetried(t *testing.T) {
	first := mustDesiredList(t, "api.example.test\n")
	second := mustDesiredList(t, "other.example.test\n")
	settings := &fakeSettings{states: []serviceSnapshot{
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
	settings := &fakeSettings{states: []serviceSnapshot{{ServiceName: "Wi-Fi"}}}
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
	if got := module.PublicationGeneration(); got < 3 {
		t.Fatalf("retry did not consume a new generation: %d", got)
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
		if module.PublicationGeneration() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("publication generation did not reach %d", want)
}

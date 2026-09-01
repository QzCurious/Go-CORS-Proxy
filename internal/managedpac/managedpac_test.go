package managedpac

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/lib/networkservice"
)

type fakeServices struct {
	mu            sync.Mutex
	states        []fakeServiceState
	discoveryErr  error
	observeErrors map[string]error
	setErrors     map[string]error
	beforeObserve func(*fakeServices)
	beforeDisable func(*fakeServices)
	writes        []string
	disableErr    error
	disableErrors map[string]error
}

type fakeService struct {
	owner *fakeServices
	name  string
}

type fakeServiceState struct {
	ServiceName string
	URL         string
	Enabled     bool
}

func (f *fakeServices) List(context.Context) ([]networkservice.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.discoveryErr != nil {
		return nil, f.discoveryErr
	}
	services := make([]networkservice.Service, 0, len(f.states))
	for _, state := range f.states {
		services = append(services, &fakeService{owner: f, name: state.ServiceName})
	}
	return services, nil
}

func (s *fakeService) Name() string { return s.name }

func (s *fakeService) PAC(_ context.Context) (networkservice.PACSetting, error) {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	if s.owner.beforeObserve != nil {
		beforeObserve := s.owner.beforeObserve
		s.owner.beforeObserve = nil
		beforeObserve(s.owner)
	}
	if err := s.owner.observeErrors[s.name]; err != nil {
		return networkservice.PACSetting{}, err
	}
	for _, state := range s.owner.states {
		if state.ServiceName == s.name {
			return networkservice.PACSetting{URL: state.URL, Enabled: state.Enabled}, nil
		}
	}
	return networkservice.PACSetting{}, errors.New("network service not found")
}

func (s *fakeService) SetPAC(ctx context.Context, pacURL string) error {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.owner.setErrors[s.name]; err != nil {
		return err
	}
	for index := range s.owner.states {
		if s.owner.states[index].ServiceName == s.name {
			s.owner.states[index].URL = pacURL
			s.owner.states[index].Enabled = true
			s.owner.writes = append(s.owner.writes, s.name+"="+pacURL)
			return nil
		}
	}
	return errors.New("network service not found")
}

func (s *fakeService) DisablePAC(_ context.Context) error {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	if s.owner.beforeDisable != nil {
		beforeDisable := s.owner.beforeDisable
		s.owner.beforeDisable = nil
		beforeDisable(s.owner)
	}
	if s.owner.disableErr != nil {
		return s.owner.disableErr
	}
	if err := s.owner.disableErrors[s.name]; err != nil {
		return err
	}
	for index := range s.owner.states {
		state := &s.owner.states[index]
		if state.ServiceName != s.name {
			continue
		}
		if state.Enabled {
			state.Enabled = false
		}
		return nil
	}
	return errors.New("network service not found")
}

func TestInspectClassifiesOnlyEmptyAndOwnedServicesAsManageable(t *testing.T) {
	module := &ManagedPAC{listServices: (&fakeServices{states: []fakeServiceState{
		{ServiceName: "Wi-Fi", URL: "http://corp.example/proxy.pac", Enabled: true},
		{ServiceName: "USB", URL: "", Enabled: false},
		{ServiceName: "Ethernet", URL: "http://127.0.0.1:49152/seamless-cors.pac?v=1", Enabled: false},
	}}).List}

	control, assessment, err := module.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = control.Close() })
	if got, want := assessment.ServiceSet, []string{"USB", "Ethernet"}; !slices.Equal(got, want) {
		t.Fatalf("manageable services = %v, want %v", got, want)
	}
	services := assessment.Services
	if services[0].ServiceName != "Wi-Fi" || services[0].Ownership != OwnershipForeign {
		t.Fatalf("services not ordered or classified: %#v", services)
	}
}

func TestInspectRetainsServiceWithPACObservationIssue(t *testing.T) {
	settings := &fakeServices{
		states: []fakeServiceState{
			{ServiceName: "Wi-Fi"},
			{ServiceName: "VPN"},
		},
		observeErrors: map[string]error{"VPN": errors.New("PAC query failed")},
	}

	control, assessment, err := (&ManagedPAC{listServices: settings.List}).Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = control.Close() })
	if got, want := assessment.ServiceSet, []string{"Wi-Fi"}; !slices.Equal(got, want) {
		t.Fatalf("manageable services = %v, want %v", got, want)
	}
	services := assessment.Services
	if len(services) != 2 || services[1].ServiceName != "VPN" {
		t.Fatalf("services = %#v", services)
	}
	if len(assessment.ObservationIssues) != 1 || assessment.ObservationIssues[0].Diagnostic != "PAC query failed" {
		t.Fatalf("observation issues = %#v", assessment.ObservationIssues)
	}
}

func TestInspectDoesNotClassifyCancellationAsObservationIssue(t *testing.T) {
	settings := &fakeServices{
		states:        []fakeServiceState{{ServiceName: "Wi-Fi"}},
		observeErrors: map[string]error{"Wi-Fi": context.Canceled},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := (&ManagedPAC{listServices: settings.List}).Begin(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("inspection error = %v", err)
	}
}

func TestControlStateRequiresAnEnabledOwnedURLForCurrentEndpoint(t *testing.T) {
	settings := &fakeServices{states: []fakeServiceState{{ServiceName: "Wi-Fi"}}}
	module := &ManagedPAC{listServices: settings.List}
	control, _, err := module.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.Deliver("127.0.0.1:8081"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = control.Close() })
	tests := []struct {
		name    string
		url     string
		enabled bool
		want    bool
	}{
		{name: "current endpoint with older generation", url: "http://127.0.0.1:8081/seamless-cors.pac?v=1", enabled: true, want: true},
		{name: "previous runtime endpoint", url: "http://127.0.0.1:8080/seamless-cors.pac?v=9", enabled: true},
		{name: "different path", url: "http://127.0.0.1:8081/other.pac", enabled: true},
		{name: "disabled", url: "http://127.0.0.1:8081/seamless-cors.pac?v=2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings.mu.Lock()
			settings.states[0].URL = tt.url
			settings.states[0].Enabled = tt.enabled
			settings.mu.Unlock()
			state, err := control.Observe()
			if err != nil {
				t.Fatal(err)
			}
			got := state.RoutesCurrentEndpoint
			if got != tt.want {
				t.Fatalf("ControlsCurrentEndpoint() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestBeginFixesOnlyInitiallyManageableServices(t *testing.T) {
	settings := &fakeServices{
		states: []fakeServiceState{
			{ServiceName: "Wi-Fi"},
			{ServiceName: "VPN"},
		},
		observeErrors: map[string]error{"VPN": errors.New("PAC query failed")},
	}
	module := &ManagedPAC{listServices: settings.List}
	control, assessment, err := module.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = control.Close() })
	if got, want := assessment.ServiceSet, []string{"Wi-Fi"}; !slices.Equal(got, want) {
		t.Fatalf("manageable services = %v, want %v", got, want)
	}
	settings.mu.Lock()
	settings.observeErrors = nil
	settings.mu.Unlock()
	state, err := control.Deliver("127.0.0.1:49152")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := state.ServiceSet, []string{"Wi-Fi"}; !slices.Equal(got, want) {
		t.Fatalf("controlled service set = %v, want %v", got, want)
	}
	if len(settings.writes) != 1 || !strings.HasPrefix(settings.writes[0], "Wi-Fi=") {
		t.Fatalf("writes = %#v, want only Wi-Fi", settings.writes)
	}
}

func TestControlLifetimeOutlivesNewContextAndCloseCleansOwnedState(t *testing.T) {
	settings := &fakeServices{states: []fakeServiceState{{ServiceName: "Wi-Fi"}}}
	module := &ManagedPAC{listServices: settings.List}
	ctx, cancel := context.WithCancel(context.Background())
	control, _, err := module.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()

	if _, err := control.Deliver("127.0.0.1:8081"); err != nil {
		t.Fatalf("Set after creation context cancellation: %v", err)
	}
	if _, err := control.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	settings.mu.Lock()
	enabled := settings.states[0].Enabled
	settings.mu.Unlock()
	if enabled {
		t.Fatal("Close left marker-owned PAC active")
	}

}

func TestClosedControlRejectsOperations(t *testing.T) {
	module := &ManagedPAC{listServices: (&fakeServices{states: []fakeServiceState{{ServiceName: "Wi-Fi"}}}).List}
	control, _, err := module.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.Close(); err != nil {
		t.Fatal(err)
	}

	_, setErr := control.Deliver("127.0.0.1:8081")
	_, stateErr := control.Observe()
	var closedErr controlClosedError
	if !errors.As(setErr, &closedErr) || !errors.As(stateErr, &closedErr) {
		t.Fatalf("Deliver error = %v, Observe error = %v; want controlClosedError", setErr, stateErr)
	}
}

func TestCleanupSucceedsWithUnobservableOwnedPAC(t *testing.T) {
	settings := &fakeServices{
		states:        []fakeServiceState{{ServiceName: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true}},
		observeErrors: map[string]error{"Wi-Fi": errors.New("PAC query failed")},
	}

	report, err := (&ManagedPAC{listServices: settings.List}).Cleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.ObservationIssues) != 1 || report.ObservationIssues[0].ServiceName != "Wi-Fi" {
		t.Fatalf("observation issues = %#v", report.ObservationIssues)
	}
	if !settings.states[0].Enabled {
		t.Fatal("cleanup mutated an unobservable PAC setting")
	}
}

func TestFootprintDistinguishesOwnedURLFromActiveOwnedState(t *testing.T) {
	module := &ManagedPAC{listServices: (&fakeServices{states: []fakeServiceState{{
		ServiceName: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: false,
	}}}).List}
	report, err := module.InspectFootprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.State != FootprintNone {
		t.Fatalf("footprint state = %q, want none for disabled retained URL", report.State)
	}
}

func TestCleanupContinuesAndReportsPerServiceFailures(t *testing.T) {
	settings := &fakeServices{
		states: []fakeServiceState{
			{ServiceName: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true},
			{ServiceName: "USB", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true},
		},
		disableErrors: map[string]error{"Wi-Fi": errors.New("permission denied")},
	}
	module := &ManagedPAC{listServices: settings.List}

	_, err := module.Cleanup(context.Background())
	var cleanupErr activeStateCleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("cleanup error = %v, want activeStateCleanupError", err)
	}
	if len(cleanupErr.serviceFailures) != 1 || cleanupErr.serviceFailures[0].serviceName != "Wi-Fi" {
		t.Fatalf("service failures = %#v", cleanupErr.serviceFailures)
	}
	if !slices.Equal(cleanupErr.remainingServices, []string{"Wi-Fi"}) {
		t.Fatalf("remaining services = %v, want Wi-Fi", cleanupErr.remainingServices)
	}
	settings.mu.Lock()
	defer settings.mu.Unlock()
	if settings.states[1].Enabled {
		t.Fatal("USB cleanup was not attempted after Wi-Fi failed")
	}
}

func TestCleanupPreservesForeignStateThatAppearsAfterInspection(t *testing.T) {
	settings := &fakeServices{
		states: []fakeServiceState{{ServiceName: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true}},
		beforeObserve: func(settings *fakeServices) {
			settings.states[0].URL = "http://corp.example/proxy.pac"
		},
	}
	module := &ManagedPAC{listServices: settings.List}

	if _, err := module.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	settings.mu.Lock()
	defer settings.mu.Unlock()
	if !settings.states[0].Enabled || settings.states[0].URL != "http://corp.example/proxy.pac" {
		t.Fatalf("foreign setting was changed: %#v", settings.states[0])
	}
}

func TestCleanupDisablesFreshlyObservedOwnedState(t *testing.T) {
	settings := &fakeServices{
		states: []fakeServiceState{{ServiceName: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac?v=1", Enabled: true}},
		beforeObserve: func(settings *fakeServices) {
			settings.states[0].URL = "http://127.0.0.1/seamless-cors.pac?v=2"
		},
	}
	module := &ManagedPAC{listServices: settings.List}

	if _, err := module.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	settings.mu.Lock()
	defer settings.mu.Unlock()
	if settings.states[0].Enabled {
		t.Fatalf("freshly observed owned state was not disabled: %#v", settings.states[0])
	}
}

func TestCleanupConcealsMutationFailureWithoutOwnedResidue(t *testing.T) {
	settings := &fakeServices{
		states:     []fakeServiceState{{ServiceName: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true}},
		disableErr: errors.New("network service disappeared"),
		beforeDisable: func(settings *fakeServices) {
			settings.states[0].URL = "http://corp.example/proxy.pac"
		},
	}
	module := &ManagedPAC{listServices: settings.List}

	if _, err := module.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup error = %v, want concealed after verified foreign state", err)
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

func TestSetAdvancesGenerationOnEveryCall(t *testing.T) {
	settings := &fakeServices{states: []fakeServiceState{{ServiceName: "Wi-Fi"}}}
	module := &ManagedPAC{listServices: settings.List}
	control, _, err := module.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = control.Close() })
	if _, err := control.Deliver("127.0.0.1:8081"); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Deliver("127.0.0.1:8081"); err != nil {
		t.Fatal(err)
	}
	if len(settings.writes) != 2 || !strings.Contains(settings.writes[1], "v=2") {
		t.Fatalf("writes = %v, want second generation", settings.writes)
	}
}

func TestDeliveryPreservesForeignStateThatAppearsAfterInspection(t *testing.T) {
	settings := &fakeServices{states: []fakeServiceState{{ServiceName: "Wi-Fi"}}}
	module := &ManagedPAC{listServices: settings.List}
	control, _, err := module.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = control.Close() })
	settings.states[0] = fakeServiceState{ServiceName: "Wi-Fi", URL: "http://corp.example/proxy.pac", Enabled: true}
	state, err := control.Deliver("127.0.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	if warnings := serviceWarnings(state, "Wi-Fi"); len(warnings) != 1 || warnings[0].Kind != WarningDrift {
		t.Fatalf("warnings = %#v", warnings)
	}
	settings.mu.Lock()
	defer settings.mu.Unlock()
	if len(settings.writes) != 0 || settings.states[0].URL != "http://corp.example/proxy.pac" {
		t.Fatalf("foreign setting was overwritten: state=%#v writes=%v", settings.states[0], settings.writes)
	}
}

func TestDeliveryUpdatesFreshlyObservedOwnedState(t *testing.T) {
	settings := &fakeServices{states: []fakeServiceState{{ServiceName: "Wi-Fi"}}}
	module := &ManagedPAC{listServices: settings.List}
	control, _, err := module.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = control.Close() })
	settings.states[0].URL = "http://127.0.0.1:8081/seamless-cors.pac?v=0"
	state, err := control.Deliver("127.0.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	if warnings := serviceWarnings(state, "Wi-Fi"); len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if len(settings.writes) != 1 || !strings.Contains(settings.writes[0], "seamless-cors.pac?v=1") {
		t.Fatalf("writes = %v, want generation 1", settings.writes)
	}
}

func TestPartialSetFailureDoesNotRollBackSuccessOrRetry(t *testing.T) {
	settings := &fakeServices{states: []fakeServiceState{
		{ServiceName: "Ethernet"},
		{ServiceName: "Wi-Fi"},
	}}
	module := &ManagedPAC{listServices: settings.List}
	control, _, err := module.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = control.Close() })
	settings.setErrors = map[string]error{"Ethernet": errors.New("write denied")}
	state, err := control.Deliver("127.0.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	if warnings := serviceWarnings(state, "Ethernet"); len(warnings) != 1 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if !state.RoutesCurrentEndpoint || len(settings.writes) != 1 || !strings.HasPrefix(settings.writes[0], "Wi-Fi=") {
		t.Fatalf("state = %#v, writes = %v", state, settings.writes)
	}
	settings.setErrors = nil
	if _, err := control.Observe(); err != nil {
		t.Fatal(err)
	}
	if len(settings.writes) != 1 {
		t.Fatalf("State retried delivery: writes = %v", settings.writes)
	}
}

func TestFailedSetConsumesGenerationAndRecoveryRequiresAnotherSet(t *testing.T) {
	settings := &fakeServices{states: []fakeServiceState{{ServiceName: "Wi-Fi"}}}
	module := &ManagedPAC{listServices: settings.List}
	control, _, err := module.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = control.Close() })
	if _, err := control.Deliver("127.0.0.1:8081"); err != nil {
		t.Fatal(err)
	}
	settings.setErrors = map[string]error{"Wi-Fi": errors.New("write denied")}
	failed, err := control.Deliver("127.0.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	if warnings := serviceWarnings(failed, "Wi-Fi"); len(warnings) != 1 || warnings[0].Kind != WarningUpdateFailed {
		t.Fatalf("warnings = %#v", warnings)
	}
	settings.setErrors = nil
	if _, err := control.Observe(); err != nil {
		t.Fatal(err)
	}
	if len(settings.writes) != 1 {
		t.Fatalf("State retried failed Set: writes = %v", settings.writes)
	}
	if _, err := control.Deliver("127.0.0.1:8081"); err != nil {
		t.Fatal(err)
	}
	if len(settings.writes) != 2 || !strings.Contains(settings.writes[1], "v=3") {
		t.Fatalf("writes after explicit recovery Set = %v", settings.writes)
	}
}

func TestSetReplacesPriorDeliveryWarnings(t *testing.T) {
	settings := &fakeServices{states: []fakeServiceState{{ServiceName: "Wi-Fi"}}}
	module := &ManagedPAC{listServices: settings.List}
	control, _, err := module.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = control.Close() })
	settings.states[0].URL = "http://corp.example/proxy.pac"
	drift, err := control.Deliver("127.0.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	if warnings := serviceWarnings(drift, "Wi-Fi"); len(warnings) != 1 || warnings[0].Kind != WarningDrift {
		t.Fatalf("drift warnings = %#v", warnings)
	}
	settings.states[0].URL = ""
	settings.states[0].Enabled = false
	settings.setErrors = map[string]error{"Wi-Fi": errors.New("write denied")}
	failed, err := control.Deliver("127.0.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	if warnings := serviceWarnings(failed, "Wi-Fi"); len(warnings) != 1 || warnings[0].Kind != WarningUpdateFailed {
		t.Fatalf("failure warnings = %#v", warnings)
	}
	settings.setErrors = nil
	recovered, err := control.Deliver("127.0.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	if warnings := serviceWarnings(recovered, "Wi-Fi"); len(warnings) != 0 {
		t.Fatalf("recovery warnings = %#v, want cleared", warnings)
	}
}

func serviceWarnings(state ControlState, serviceName string) []Warning {
	var warnings []Warning
	for _, warning := range state.Warnings {
		if warning.ServiceName == serviceName {
			warnings = append(warnings, warning)
		}
	}
	return warnings
}

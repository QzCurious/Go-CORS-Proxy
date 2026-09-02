package systempac

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/lib/networkservice"
)

type fakeSystem struct {
	mu           sync.Mutex
	states       []ServiceState
	discoveryErr error
	observeErrs  map[string]error
	setErrs      map[string]error
	disableErrs  map[string]error
	writes       []string
	discoveries  int
}

func (f *fakeSystem) list(context.Context) ([]networkservice.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discoveries++
	services := make([]networkservice.Service, 0, len(f.states))
	for _, state := range f.states {
		services = append(services, fakeService{system: f, name: state.Name})
	}
	return services, f.discoveryErr
}

type fakeService struct {
	system *fakeSystem
	name   string
}

func (f fakeService) Name() string { return f.name }
func (f fakeService) PAC(context.Context) (networkservice.PACSetting, error) {
	f.system.mu.Lock()
	defer f.system.mu.Unlock()
	if err := f.system.observeErrs[f.name]; err != nil {
		return networkservice.PACSetting{}, err
	}
	for _, state := range f.system.states {
		if state.Name == f.name {
			return networkservice.PACSetting{URL: state.URL, Enabled: state.Enabled}, nil
		}
	}
	return networkservice.PACSetting{}, errors.New("missing")
}
func (f fakeService) SetPAC(_ context.Context, raw string) error {
	f.system.mu.Lock()
	defer f.system.mu.Unlock()
	if err := f.system.setErrs[f.name]; err != nil {
		return err
	}
	for i := range f.system.states {
		if f.system.states[i].Name == f.name {
			f.system.states[i].URL = raw
			f.system.states[i].Enabled = true
			f.system.writes = append(f.system.writes, f.name+"="+raw)
			return nil
		}
	}
	return errors.New("missing")
}
func (f fakeService) DisablePAC(context.Context) error {
	f.system.mu.Lock()
	defer f.system.mu.Unlock()
	if err := f.system.disableErrs[f.name]; err != nil {
		return err
	}
	for i := range f.system.states {
		if f.system.states[i].Name == f.name {
			f.system.states[i].Enabled = false
			return nil
		}
	}
	return errors.New("missing")
}

func TestDeliverFreshlyDiscoversSafeServicesAndConsumesGenerations(t *testing.T) {
	system := &fakeSystem{states: []ServiceState{{Name: "Wi-Fi", Ownership: OwnershipEmpty}}}
	module := &SystemPAC{listServices: system.list}
	first, err := module.Deliver(context.Background(), "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	system.mu.Lock()
	system.states = append(system.states, ServiceState{Name: "USB", Ownership: OwnershipEmpty})
	system.mu.Unlock()
	second, err := module.Deliver(context.Background(), "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation != 1 || second.Generation != 2 {
		t.Fatalf("generations = %d, %d", first.Generation, second.Generation)
	}
	if len(second.Services) != 2 || len(system.writes) != 3 {
		t.Fatalf("state = %#v, writes = %#v", second, system.writes)
	}
	if !strings.Contains(system.writes[2], "v=2") {
		t.Fatalf("second delivery URL = %q", system.writes[2])
	}
}

func TestDeliverPreservesForeignAndReturnsPartialFactsWithConcreteErrors(t *testing.T) {
	system := &fakeSystem{states: []ServiceState{
		{Name: "Corporate", URL: "http://corp/pac", Enabled: true},
		{Name: "Wi-Fi", Ownership: OwnershipEmpty},
		{Name: "VPN", Ownership: OwnershipEmpty},
	}, setErrs: map[string]error{"VPN": errors.New("denied")}}
	state, err := (&SystemPAC{listServices: system.list}).Deliver(context.Background(), "127.0.0.1:8080")
	var mutation MutationError
	if !errors.As(err, &mutation) || mutation.ServiceName != "VPN" {
		t.Fatalf("error = %v", err)
	}
	if state.Generation != 1 || len(state.Services) != 3 {
		t.Fatalf("state = %#v", state)
	}
	if state.Services[0].Ownership != OwnershipForeign {
		t.Fatalf("foreign state = %#v", state.Services[0])
	}
	if !slices.Equal(system.writes, []string{"Wi-Fi=http://127.0.0.1:8080/seamless-cors.pac?v=1"}) {
		t.Fatalf("writes = %#v", system.writes)
	}
}

func TestObserveIsFreshAndOwnerlessNeverClaimsCurrentEndpoint(t *testing.T) {
	system := &fakeSystem{states: []ServiceState{{Name: "Wi-Fi", URL: "http://127.0.0.1:8080/seamless-cors.pac?v=8", Enabled: true}}}
	module := &SystemPAC{listServices: system.list}
	ownerless, err := module.Observe(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	current, err := module.Observe(context.Background(), "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	if ownerless.RoutesCurrentEndpoint || !current.RoutesCurrentEndpoint {
		t.Fatalf("ownerless/current = %t/%t", ownerless.RoutesCurrentEndpoint, current.RoutesCurrentEndpoint)
	}
}

func TestCleanupPreservesForeignAndDisabledOwnedAndReportsUncertainty(t *testing.T) {
	system := &fakeSystem{states: []ServiceState{
		{Name: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true},
		{Name: "Disabled", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: false},
		{Name: "Corporate", URL: "http://corp/pac", Enabled: true},
		{Name: "VPN", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true},
	}, observeErrs: map[string]error{"VPN": errors.New("query failed")}}
	states, err := (&SystemPAC{listServices: system.list}).Cleanup(context.Background())
	var observation ObservationError
	if !errors.As(err, &observation) || len(states) != 4 {
		t.Fatalf("states/error = %#v / %v", states, err)
	}
	if system.states[0].Enabled || !system.states[2].Enabled || !system.states[3].Enabled {
		t.Fatalf("states = %#v", system.states)
	}
}

func TestFailedDiscoveryStillConsumesDeliveryGeneration(t *testing.T) {
	system := &fakeSystem{discoveryErr: errors.New("unavailable")}
	module := &SystemPAC{listServices: system.list}
	first, err := module.Deliver(context.Background(), "127.0.0.1:8080")
	var discovery DiscoveryError
	if !errors.As(err, &discovery) || first.Generation != 1 {
		t.Fatalf("first = %#v, %v", first, err)
	}
	system.discoveryErr = nil
	second, err := module.Deliver(context.Background(), "127.0.0.1:8080")
	if err != nil || second.Generation != 2 {
		t.Fatalf("second = %#v, %v", second, err)
	}
}

func TestInvalidEndpointIsNotDeliveryAttempt(t *testing.T) {
	system := &fakeSystem{}
	module := &SystemPAC{listServices: system.list}
	state, err := module.Deliver(context.Background(), "")
	if err == nil || state.Generation != 0 || system.discoveries != 0 {
		t.Fatalf("state/error/discoveries = %#v / %v / %d", state, err, system.discoveries)
	}
	state, err = module.Deliver(context.Background(), "127.0.0.1:8080")
	if err != nil || state.Generation != 1 {
		t.Fatalf("valid delivery = %#v / %v", state, err)
	}
}

func TestPartialDiscoveryRetainsAndProcessesAvailableServices(t *testing.T) {
	system := &fakeSystem{states: []ServiceState{{Name: "Wi-Fi"}}, discoveryErr: errors.New("one adapter failed")}
	state, err := (&SystemPAC{listServices: system.list}).Deliver(context.Background(), "127.0.0.1:8080")
	var discovery DiscoveryError
	if !errors.As(err, &discovery) || len(state.Services) != 1 || state.Services[0].Ownership != OwnershipOwned {
		t.Fatalf("state/error = %#v / %v", state, err)
	}
}

package managedpac

import (
	"errors"
	"strings"
	"testing"

	"seamless-cors/internal/platform"
)

type footprintFakeAdapter struct {
	states       []platform.PACServiceState
	inspectErr   error
	clearErr     error
	changedState *platform.PACServiceState
	clearCalls   int
}

func (f *footprintFakeAdapter) CurrentPACState() ([]platform.PACServiceState, error) {
	if f.inspectErr != nil {
		return nil, f.inspectErr
	}
	return append([]platform.PACServiceState(nil), f.states...), nil
}

func (f *footprintFakeAdapter) ClearPACIfMatches(expected []platform.PACServiceState) error {
	f.clearCalls++
	if f.clearErr != nil {
		return f.clearErr
	}
	if f.changedState != nil {
		f.states[0] = *f.changedState
		f.changedState = nil
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

func TestOwnedURLMatchesOnlyLoopbackHTTPPACFilename(t *testing.T) {
	tests := map[string]bool{
		"http://127.0.0.1:8079/seamless-cors.pac":               true,
		"http://127.0.0.1:8079/seamless-cors.pac?v=2":           true,
		"http://localhost:8079/nested/seamless-cors.pac":        true,
		"http://[::1]:8079/seamless-cors.pac":                   true,
		"http://127.0.0.1:8079/not-seamless-cors.pac":           false,
		"http://127.0.0.1:8079/seamless-cors.pac.backup":        false,
		"https://127.0.0.1:8079/seamless-cors.pac":              false,
		"http://proxy.example.test/seamless-cors.pac":           false,
		"http://proxy.example.test/corporate-seamless-cors.pac": false,
	}
	for raw, want := range tests {
		if got := IsOwnedURL(raw); got != want {
			t.Fatalf("IsOwnedURL(%q) = %t, want %t", raw, got, want)
		}
	}
}

func TestInspectFootprintIgnoresDisabledOwnedURL(t *testing.T) {
	adapter := &footprintFakeAdapter{states: []platform.PACServiceState{{
		Name: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: false,
	}}}
	inspection, err := InspectFootprint(adapter)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Needed() {
		t.Fatal("disabled owned PAC footprint should not require cleanup")
	}
}

func TestClearFootprintRemovesOnlyExpectedOwnedState(t *testing.T) {
	adapter := &footprintFakeAdapter{states: []platform.PACServiceState{
		{Name: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true},
		{Name: "VPN", URL: "http://corp.example/proxy.pac", Enabled: true},
	}}
	if err := ClearFootprint(adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.states[0].Enabled || !adapter.states[1].Enabled {
		t.Fatalf("states after cleanup = %#v", adapter.states)
	}
}

func TestClearFootprintPreservesStateChangedAfterInspection(t *testing.T) {
	foreign := platform.PACServiceState{Name: "Wi-Fi", URL: "http://corp.example/proxy.pac", Enabled: true}
	adapter := &footprintFakeAdapter{
		states:       []platform.PACServiceState{{Name: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true}},
		changedState: &foreign,
	}
	if err := ClearFootprint(adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.states[0] != foreign {
		t.Fatalf("changed PAC state = %#v", adapter.states[0])
	}
}

func TestClearFootprintFailsVerificationWhenChangedStateIsStillOwned(t *testing.T) {
	changed := platform.PACServiceState{Name: "Wi-Fi", URL: "http://127.0.0.1:9000/seamless-cors.pac?v=2", Enabled: true}
	adapter := &footprintFakeAdapter{
		states:       []platform.PACServiceState{{Name: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true}},
		changedState: &changed,
	}
	err := ClearFootprint(adapter)
	if err == nil || !strings.Contains(err.Error(), "footprint remains") {
		t.Fatalf("verification error = %v", err)
	}
	if adapter.states[0] != changed {
		t.Fatalf("changed owned PAC state = %#v", adapter.states[0])
	}
}

func TestClearFootprintReportsInspectionAndClearFailures(t *testing.T) {
	adapter := &footprintFakeAdapter{inspectErr: errors.New("inspection denied")}
	if err := ClearFootprint(adapter); err == nil || !strings.Contains(err.Error(), "inspection denied") {
		t.Fatalf("inspection error = %v", err)
	}
	adapter.inspectErr = nil
	adapter.states = []platform.PACServiceState{{Name: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true}}
	adapter.clearErr = errors.New("clear denied")
	if err := ClearFootprint(adapter); err == nil || !strings.Contains(err.Error(), "clear denied") {
		t.Fatalf("clear error = %v", err)
	}
}

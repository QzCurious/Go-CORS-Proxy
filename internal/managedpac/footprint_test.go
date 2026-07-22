package managedpac

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type footprintFakeAdapter struct {
	states       []ServiceSnapshot
	inspectErr   error
	clearErr     error
	changedState *ServiceSnapshot
	clearCalls   int
}

func (f *footprintFakeAdapter) Snapshot(context.Context) ([]ServiceSnapshot, error) {
	if f.inspectErr != nil {
		return nil, f.inspectErr
	}
	return append([]ServiceSnapshot(nil), f.states...), nil
}

func (f *footprintFakeAdapter) Apply(context.Context, string, []string) (ApplyResult, error) {
	return ApplyResult{}, nil
}

func (f *footprintFakeAdapter) ClearIfUnchanged(_ context.Context, expected []ServiceSnapshot) error {
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
	adapter := &footprintFakeAdapter{states: []ServiceSnapshot{{
		ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: false,
	}}}
	inspection, err := InspectFootprint(context.Background(), adapter)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Needed() {
		t.Fatal("disabled owned PAC footprint should not require cleanup")
	}
}

func TestClearFootprintRemovesOnlyExpectedOwnedState(t *testing.T) {
	adapter := &footprintFakeAdapter{states: []ServiceSnapshot{
		{ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true},
		{ServiceName: "VPN", PACURL: "http://corp.example/proxy.pac", Enabled: true},
	}}
	if err := ClearFootprint(context.Background(), adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.states[0].Enabled || !adapter.states[1].Enabled {
		t.Fatalf("states after cleanup = %#v", adapter.states)
	}
}

func TestClearFootprintPreservesStateChangedAfterInspection(t *testing.T) {
	foreign := ServiceSnapshot{ServiceName: "Wi-Fi", PACURL: "http://corp.example/proxy.pac", Enabled: true}
	adapter := &footprintFakeAdapter{
		states:       []ServiceSnapshot{{ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true}},
		changedState: &foreign,
	}
	if err := ClearFootprint(context.Background(), adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.states[0] != foreign {
		t.Fatalf("changed PAC state = %#v", adapter.states[0])
	}
}

func TestClearFootprintFailsVerificationWhenChangedStateIsStillOwned(t *testing.T) {
	changed := ServiceSnapshot{ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1:9000/seamless-cors.pac?v=2", Enabled: true}
	adapter := &footprintFakeAdapter{
		states:       []ServiceSnapshot{{ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true}},
		changedState: &changed,
	}
	err := ClearFootprint(context.Background(), adapter)
	if err == nil || !strings.Contains(err.Error(), "footprint remains") {
		t.Fatalf("verification error = %v", err)
	}
	if adapter.states[0] != changed {
		t.Fatalf("changed owned PAC state = %#v", adapter.states[0])
	}
}

func TestClearFootprintReportsInspectionAndClearFailures(t *testing.T) {
	adapter := &footprintFakeAdapter{inspectErr: errors.New("inspection denied")}
	if err := ClearFootprint(context.Background(), adapter); err == nil || !strings.Contains(err.Error(), "inspection denied") {
		t.Fatalf("inspection error = %v", err)
	}
	adapter.inspectErr = nil
	adapter.states = []ServiceSnapshot{{ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1/seamless-cors.pac", Enabled: true}}
	adapter.clearErr = errors.New("clear denied")
	if err := ClearFootprint(context.Background(), adapter); err == nil || !strings.Contains(err.Error(), "clear denied") {
		t.Fatalf("clear error = %v", err)
	}
}

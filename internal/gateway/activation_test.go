package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"seamless-cors/internal/platform"
)

func TestExecuteStartBindsCollectiveConsentToForeignPACState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.WriteFile(domainPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	config := "domain-list: " + domainPath + "\nca-trusted: false\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	adapter := &lifecycleTestAdapter{states: []platform.PACServiceState{
		{Name: "Wi-Fi", Enabled: true, URL: "http://corp.example/a.pac"},
		{Name: "Ethernet"},
	}}
	lifecycle, err := newLifecycle(adapter, newCoordinator(filepath.Join(configDir, "runtime")), "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind != StartResultConsentRequired || first.PACReplacementConsent == nil {
		t.Fatalf("first start = %#v", first)
	}
	detail := first.PACReplacementConsent
	if len(detail.CurrentPACState) != 2 {
		t.Fatalf("managed service detail = %#v", detail.CurrentPACState)
	}
	if !detail.CurrentPACState[1].ReplacementConsentRequired || detail.CurrentPACState[0].ReplacementConsentRequired {
		t.Fatalf("replacement consent markers = %#v", detail.CurrentPACState)
	}
	if detail.Fingerprint == "" {
		t.Fatal("consent fingerprint is empty")
	}

	adapter.states[1].URL = "http://corp.example/b.pac"
	changed, err := lifecycle.ExecuteStart(context.Background(), StartRequest{
		PACReplacementConsent: &PACReplacementConsentInput{Accepted: true, Fingerprint: detail.Fingerprint},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Kind != StartResultConsentRequired || changed.PACReplacementConsent == nil {
		t.Fatalf("changed-state retry = %#v", changed)
	}
	if changed.PACReplacementConsent.Fingerprint == detail.Fingerprint {
		t.Fatal("changed foreign PAC URL retained old consent fingerprint")
	}
	if adapter.applied != 0 {
		t.Fatalf("state-mismatched consent applied PAC %d times", adapter.applied)
	}
}

type lifecycleTestAdapter struct {
	states   []platform.PACServiceState
	applied  int
	stateErr error
	clearErr error
	cleared  int
}

func (f *lifecycleTestAdapter) Capabilities() platform.CapabilityReport {
	return platform.CapabilityReport{
		Platform:          "test/test",
		Supported:         true,
		PACManagement:     platform.CapabilitySupported,
		CATrustManagement: platform.CapabilitySupported,
		RuntimeCleanup:    platform.CapabilitySupported,
	}
}

func (f *lifecycleTestAdapter) ApplyPAC(_ string, services []string) ([]platform.PACServiceUpdate, error) {
	f.applied++
	updates := make([]platform.PACServiceUpdate, 0, len(services))
	for _, service := range services {
		updates = append(updates, platform.PACServiceUpdate{ServiceName: service, Outcome: platform.PACApplyOutcomeApplied})
	}
	return updates, nil
}

func (f *lifecycleTestAdapter) CurrentPACState() ([]platform.PACServiceState, error) {
	if f.stateErr != nil {
		return nil, f.stateErr
	}
	return append([]platform.PACServiceState(nil), f.states...), nil
}

func (f *lifecycleTestAdapter) ClearPACIfMatches(expected []platform.PACServiceState) error {
	f.cleared++
	if f.clearErr != nil {
		return f.clearErr
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
func (f *lifecycleTestAdapter) TrustedCAs() ([]platform.CARecord, error)  { return nil, nil }
func (f *lifecycleTestAdapter) TrustCA(context.Context, []byte) error     { return nil }
func (f *lifecycleTestAdapter) RemoveCAs(context.Context, []string) error { return nil }

func TestExecuteStartReportsEarlyCleanupFailureAsStructuredOutcome(t *testing.T) {
	adapter := &lifecycleTestAdapter{
		states: []platform.PACServiceState{{
			Name: "Wi-Fi", URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true,
		}},
		clearErr: errors.New("cleanup denied"),
	}
	lifecycle, err := newLifecycle(adapter, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StartResultCleanupFailed {
		t.Fatalf("start kind = %s, want %s", result.Kind, StartResultCleanupFailed)
	}
	if len(result.CleanupFailures) != 1 || result.CleanupFailures[0].Subject != CleanupSubjectManagedPAC {
		t.Fatalf("cleanup failures = %#v", result.CleanupFailures)
	}
}

package gatewayfacade

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"seamless-cors/internal/gatewaycoord"
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

	adapter := &facadeTestAdapter{states: []platform.PACServiceState{
		{Name: "Wi-Fi", Enabled: true, URL: "http://corp.example/a.pac"},
		{Name: "Ethernet"},
	}}
	facade, err := New(adapter, gatewaycoord.New(filepath.Join(configDir, "runtime")), "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := facade.ExecuteStart(context.Background(), StartRequest{})
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
	changed, err := facade.ExecuteStart(context.Background(), StartRequest{
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

type facadeTestAdapter struct {
	states  []platform.PACServiceState
	applied int
}

func (f *facadeTestAdapter) Capabilities() platform.CapabilityReport {
	return platform.CapabilityReport{
		Platform:          "test/test",
		Supported:         true,
		PACManagement:     platform.CapabilitySupported,
		CATrustManagement: platform.CapabilitySupported,
		RuntimeCleanup:    platform.CapabilitySupported,
	}
}

func (f *facadeTestAdapter) ApplyPAC(_ string, services []string) ([]platform.PACServiceUpdate, error) {
	f.applied++
	updates := make([]platform.PACServiceUpdate, 0, len(services))
	for _, service := range services {
		updates = append(updates, platform.PACServiceUpdate{ServiceName: service, Outcome: platform.PACApplyOutcomeApplied})
	}
	return updates, nil
}

func (f *facadeTestAdapter) CurrentPACState() ([]platform.PACServiceState, error) {
	return append([]platform.PACServiceState(nil), f.states...), nil
}

func (f *facadeTestAdapter) ClearOwnedPAC() error                      { return nil }
func (f *facadeTestAdapter) TrustedCAs() ([]platform.CARecord, error)  { return nil, nil }
func (f *facadeTestAdapter) TrustCA(context.Context, []byte) error     { return nil }
func (f *facadeTestAdapter) RemoveCAs(context.Context, []string) error { return nil }

package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

func TestExecuteStartBindsCollectiveConsentToForeignPACState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	upstreamPath := filepath.Join(configDir, "upstreams.txt")
	if err := os.WriteFile(upstreamPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	config := "upstream-list: " + upstreamPath + "\nca-trusted: false\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	settings := &lifecycleTestSystemSettings{states: []managedpac.ServiceSnapshot{
		{ServiceName: "Wi-Fi", Enabled: true, PACURL: "http://corp.example/a.pac"},
		{ServiceName: "Ethernet"},
	}}
	lifecycle, err := newLifecycle(settings, emptyTestTrustStore{}, newCoordinator(filepath.Join(configDir, "runtime")), "")
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

	settings.states[1].PACURL = "http://corp.example/b.pac"
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
	if settings.applied != 0 {
		t.Fatalf("state-mismatched consent applied PAC %d times", settings.applied)
	}
}

func TestExecuteStartReportsEarlyCleanupFailureAsStructuredOutcome(t *testing.T) {
	settings := &lifecycleTestSystemSettings{
		states: []managedpac.ServiceSnapshot{{
			ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1/seamless-cors.pac", Enabled: true,
		}},
		clearErr: errors.New("cleanup denied"),
	}
	lifecycle, err := newLifecycle(settings, emptyTestTrustStore{}, newCoordinator(t.TempDir()), "")
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

type lifecycleTestSystemSettings struct {
	states   []managedpac.ServiceSnapshot
	applied  int
	stateErr error
	clearErr error
	cleared  int
}

func (f *lifecycleTestSystemSettings) Apply(_ context.Context, _ string, services []string) (managedpac.ApplyResult, error) {
	f.applied++
	return managedpac.ApplyResult{AppliedServices: append([]string(nil), services...)}, nil
}

func (f *lifecycleTestSystemSettings) Snapshot(context.Context) ([]managedpac.ServiceSnapshot, error) {
	if f.stateErr != nil {
		return nil, f.stateErr
	}
	return append([]managedpac.ServiceSnapshot(nil), f.states...), nil
}

func (f *lifecycleTestSystemSettings) ClearIfUnchanged(_ context.Context, expected []managedpac.ServiceSnapshot) error {
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

type emptyTestTrustStore struct{}

func (emptyTestTrustStore) TrustedCertificates(context.Context) ([]userca.TrustedCertificate, error) {
	return nil, nil
}
func (emptyTestTrustStore) Trust(context.Context, []byte) error    { return nil }
func (emptyTestTrustStore) Remove(context.Context, []string) error { return nil }

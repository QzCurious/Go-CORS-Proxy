package cli

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

type appFakeAdapter struct {
	installedPAC string
	pacStates    []platform.PACServiceState
	clearedPAC   int
	trusted      int
	cleanedCA    int
	caRecords    []platform.CARecord
	trustErr     error
}

func (f *appFakeAdapter) Capabilities() platform.CapabilityReport {
	return platform.CapabilityReport{
		Platform:          "test/test",
		Supported:         true,
		PACManagement:     platform.CapabilitySupported,
		CATrustManagement: platform.CapabilitySupported,
		RuntimeCleanup:    platform.CapabilitySupported,
	}
}
func (f *appFakeAdapter) ApplyPAC(url string, services []string) ([]platform.PACServiceUpdate, error) {
	f.installedPAC = url
	if len(f.pacStates) == 0 {
		f.pacStates = []platform.PACServiceState{{Name: "Wi-Fi"}}
	}
	serviceSet := map[string]struct{}{}
	for _, service := range services {
		serviceSet[service] = struct{}{}
	}
	var updates []platform.PACServiceUpdate
	for idx := range f.pacStates {
		if _, ok := serviceSet[f.pacStates[idx].Name]; ok {
			f.pacStates[idx].URL = url
			f.pacStates[idx].Enabled = true
			updates = append(updates, platform.PACServiceUpdate{ServiceName: f.pacStates[idx].Name, Outcome: platform.PACApplyOutcomeApplied})
		}
	}
	for _, service := range services {
		found := false
		for _, update := range updates {
			found = found || update.ServiceName == service
		}
		if !found {
			updates = append(updates, platform.PACServiceUpdate{ServiceName: service, Outcome: platform.PACApplyOutcomeAbsent})
		}
	}
	return updates, nil
}
func (f *appFakeAdapter) CurrentPACState() ([]platform.PACServiceState, error) {
	if f.pacStates == nil {
		f.pacStates = []platform.PACServiceState{{Name: "Wi-Fi"}}
	}
	return f.pacStates, nil
}
func (f *appFakeAdapter) ClearPACIfMatches(expected []platform.PACServiceState) error {
	f.clearedPAC++
	for idx, state := range f.pacStates {
		for _, want := range expected {
			if state == want {
				f.pacStates[idx].Enabled = false
			}
		}
	}
	return nil
}
func (f *appFakeAdapter) TrustedCAs() ([]platform.CARecord, error) {
	return append([]platform.CARecord(nil), f.caRecords...), nil
}
func (f *appFakeAdapter) TrustCA(_ context.Context, certPEM []byte) error {
	f.trusted++
	if f.trustErr == nil {
		fingerprint, err := userca.SHA1Fingerprint(certPEM)
		if err != nil {
			return err
		}
		block, _ := pem.Decode(certPEM)
		if block == nil {
			return errors.New("invalid test cert")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return err
		}
		f.caRecords = []platform.CARecord{{SHA1: fingerprint, CertPEM: certPEM, NotAfter: cert.NotAfter}}
	}
	return f.trustErr
}
func (f *appFakeAdapter) RemoveCAs(context.Context, []string) error {
	f.cleanedCA++
	f.caRecords = nil
	return nil
}

func TestCheckReportsCapabilitiesWithoutCreatingHomeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	restoreAdapter(t, &appFakeAdapter{})
	var out bytes.Buffer

	if err := Check(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".seamless-cors")); !os.IsNotExist(err) {
		t.Fatalf("check created home config directory: %v", err)
	}
	for _, want := range []string{
		"platform: test/test",
		"supported: true",
		"pac-management: supported",
		"ca-trust-management: supported",
		"runtime-cleanup: supported",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("check output missing %q:\n%s", want, out.String())
		}
	}
}

func TestInstallIsConfigIndependentAndIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fake := &appFakeAdapter{}
	restoreAdapter(t, fake)
	var out bytes.Buffer

	if err := Install(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".seamless-cors", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("install created config: %v", err)
	}
	if fake.trusted != 1 {
		t.Fatalf("trust calls = %d", fake.trusted)
	}

	out.Reset()
	if err := Install(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if fake.trusted != 1 {
		t.Fatalf("idempotent install trust calls = %d", fake.trusted)
	}
	if !strings.Contains(out.String(), "Installed User CA is already usable.") {
		t.Fatalf("idempotent install output = %q", out.String())
	}
}

func TestUninstallAllowsUntrustedGatewayRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), testConfig{
		DomainList: domainPath,
		CATrusted:  false,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &appFakeAdapter{}
	restoreAdapter(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake)
	}()
	waitForFile(t, filepath.Join(configDir, "runtime", "gateway-state-cache.json"))
	waitForStatusOutput(t, "seamless-cors status: running")

	var out bytes.Buffer
	err := Uninstall(&out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("uninstall error = %v", err)
	}
	if !strings.Contains(out.String(), "already absent") {
		t.Fatalf("uninstall output = %q", out.String())
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUninstallAllowsRouterOnlyGatewayOwner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fake := &appFakeAdapter{}
	restoreAdapter(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- ServeWithContext(ctx, io.Discard, fake)
	}()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	var out bytes.Buffer
	if err := Uninstall(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Installed User CA is already absent.") {
		t.Fatalf("uninstall output = %q", out.String())
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

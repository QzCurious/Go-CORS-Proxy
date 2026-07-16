package cli

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"seamless-cors/internal/gatewayclient"
	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayfacade"
	"seamless-cors/internal/liveconfig"
	"seamless-cors/internal/managedpac"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

type testConfig struct {
	DomainList string
	CATrusted  bool
	SourcePath string
}

func defaultTestConfig() testConfig {
	return testConfig{
		DomainList: "~/.seamless-cors/domains.txt",
		CATrusted:  true,
	}
}

type fakeAdapter struct {
	mu               sync.Mutex
	installedPAC     string
	pacStates        []platform.PACServiceState
	clearedPAC       int
	trusted          int
	cleanedCA        int
	caRecords        []platform.CARecord
	trustErr         error
	installErr       error
	installFailAfter int
	clearErr         error
	refreshErr       error
	refreshFailAfter int
	refreshedPAC     []string
	trustStarted     chan struct{}
	releaseTrust     chan struct{}
}

func (f *fakeAdapter) Capabilities() platform.CapabilityReport {
	return platform.CapabilityReport{
		Platform:          "test/test",
		Supported:         true,
		PACManagement:     platform.CapabilitySupported,
		CATrustManagement: platform.CapabilitySupported,
		RuntimeCleanup:    platform.CapabilitySupported,
	}
}
func (f *fakeAdapter) ApplyPAC(url string, services []string) ([]platform.PACServiceUpdate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	refresh := f.installedPAC != ""
	if refresh {
		f.refreshedPAC = append(f.refreshedPAC, url)
	} else {
		f.installedPAC = url
	}
	if len(f.pacStates) == 0 {
		f.pacStates = []platform.PACServiceState{{Name: "Wi-Fi"}}
	}
	serviceSet := map[string]struct{}{}
	for _, service := range services {
		serviceSet[service] = struct{}{}
	}
	updates := make([]platform.PACServiceUpdate, 0, len(services))
	mutated := 0
	for idx := range f.pacStates {
		if _, ok := serviceSet[f.pacStates[idx].Name]; !ok {
			continue
		}
		updates = append(updates, platform.PACServiceUpdate{ServiceName: f.pacStates[idx].Name, Outcome: platform.PACApplyOutcomeApplied})
		f.pacStates[idx].URL = url
		f.pacStates[idx].Enabled = true
		mutated++
		if !refresh && f.installErr != nil && f.installFailAfter > 0 && mutated >= f.installFailAfter {
			return updates, f.installErr
		}
		if refresh && f.refreshErr != nil && f.refreshFailAfter > 0 && mutated >= f.refreshFailAfter {
			return updates, f.refreshErr
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
	if refresh && f.refreshErr != nil {
		return updates, f.refreshErr
	}
	if !refresh && f.installErr != nil {
		return updates, f.installErr
	}
	return updates, nil
}
func (f *fakeAdapter) CurrentPACState() ([]platform.PACServiceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pacStates == nil {
		f.pacStates = []platform.PACServiceState{{Name: "Wi-Fi"}}
	}
	return append([]platform.PACServiceState(nil), f.pacStates...), nil
}
func (f *fakeAdapter) ClearOwnedPAC() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearedPAC++
	for idx := range f.pacStates {
		if f.pacStates[idx].Enabled && platform.IsManagedPACFootprint(f.pacStates[idx].URL) {
			f.pacStates[idx].Enabled = false
		}
	}
	return f.clearErr
}

func (f *fakeAdapter) replacePACStates(states []platform.PACServiceState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pacStates = append([]platform.PACServiceState(nil), states...)
}

func (f *fakeAdapter) setPACState(index int, state platform.PACServiceState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pacStates[index] = state
}

func (f *fakeAdapter) appendPACState(state platform.PACServiceState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pacStates = append(f.pacStates, state)
}

func (f *fakeAdapter) installedPACSnapshot() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.installedPAC
}

func (f *fakeAdapter) refreshedPACSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.refreshedPAC...)
}
func (f *fakeAdapter) TrustedCAs() ([]platform.CARecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.CARecord(nil), f.caRecords...), nil
}
func (f *fakeAdapter) TrustCA(_ context.Context, certPEM []byte) error {
	f.mu.Lock()
	f.trusted++
	f.mu.Unlock()
	if f.trustStarted != nil {
		close(f.trustStarted)
	}
	if f.releaseTrust != nil {
		<-f.releaseTrust
	}
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
		f.mu.Lock()
		f.caRecords = []platform.CARecord{{SHA1: fingerprint, CertPEM: certPEM, NotAfter: cert.NotAfter}}
		f.mu.Unlock()
	}
	return f.trustErr
}
func (f *fakeAdapter) RemoveCAs(context.Context, []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanedCA++
	f.caRecords = nil
	return nil
}

func TestPACReplacementConsentPromptReportsManagedPACOnly(t *testing.T) {
	var out bytes.Buffer

	err := promptForPACReplacementConsentRequest(bytes.NewBufferString("yes"), &out, pacReplacementConsentRequest{
		ManagedPAC: true,
		CurrentPACState: []platform.PACServiceState{{
			Name:    "Wi-Fi",
			URL:     "http://corp.example/proxy.pac",
			Enabled: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{
		"PAC Replacement Consent required",
		"foreign -> seamless-cors owned",
		"Proceed? [y/N]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("consent prompt missing %q:\n%s", want, got)
		}
	}
}

func TestPACReplacementConsentPromptDeclineCancels(t *testing.T) {
	var out bytes.Buffer

	err := promptForPACReplacementConsentRequest(bytes.NewBufferString("no"), &out, pacReplacementConsentRequest{ManagedPAC: true})
	if !errors.Is(err, ErrPACReplacementConsentDeclined) {
		t.Fatalf("prompt error = %v", err)
	}
	if !strings.Contains(out.String(), "Gateway Activation canceled") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestManagedGatewayUsesAdapterAndCleansUpLifecycleState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), testConfig{
		DomainList: domainPath,
		CATrusted:  true,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, nil, &out, fake) }()
	waitForInstalledPAC(t, fake)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if fake.installedPAC == "" {
		t.Fatal("managed runtime did not install PAC")
	}
	if fake.trusted != 1 {
		t.Fatalf("trusted calls = %d", fake.trusted)
	}
	if fake.clearedPAC == 0 {
		t.Fatalf("marker-based PAC cleanup calls = %d", fake.clearedPAC)
	}
	if !strings.Contains(out.String(), "Installed User CA added to the current user's SSL trust settings.") {
		t.Fatalf("success output = %q", out.String())
	}
}

func TestManagedGatewayPrintsCancelMessageWhenTrustApprovalDenied(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), testConfig{
		DomainList: domainPath,
		CATrusted:  true,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{trustErr: platform.ErrTrustApprovalDenied}
	var out bytes.Buffer

	err := StartWithContextAndInput(context.Background(), nil, &out, fake)
	if !errors.Is(err, platform.ErrTrustApprovalDenied) {
		t.Fatalf("serve error = %v", err)
	}

	want := "Certificate trust was not approved.\n" +
		"Run the command again and approve the system prompt.\n"
	if out.String() != want {
		t.Fatalf("cancel output = %q", out.String())
	}
	if fake.installedPAC != "" {
		t.Fatalf("trust denial installed PAC: %q", fake.installedPAC)
	}
}

func TestManagedGatewayWaitsForCATrustApprovalBeforeActivation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), testConfig{
		DomainList: domainPath,
		CATrusted:  true,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{
		trustStarted: make(chan struct{}),
		releaseTrust: make(chan struct{}),
	}
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, nil, &out, fake) }()
	<-fake.trustStarted

	if fake.installedPAC != "" {
		t.Fatalf("pending trust approval installed PAC: %q", fake.installedPAC)
	}
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	if got := currentStatusOutput(t); !strings.Contains(got, "seamless-cors status: owner running") {
		t.Fatalf("pending trust approval status = %q", got)
	}
	if strings.Contains(out.String(), "seamless-cors running") {
		t.Fatalf("pending trust approval printed start guidance:\n%s", out.String())
	}

	close(fake.releaseTrust)
	waitForStatusOutput(t, "seamless-cors status: running")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got := out.String()
	caIndex := strings.Index(got, "Installed User CA added")
	runningIndex := strings.Index(got, "seamless-cors running")
	if caIndex < 0 || runningIndex < 0 || caIndex > runningIndex {
		t.Fatalf("start output order = %q", got)
	}
}

func TestStartRunsCleanupBeforeInvalidConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("domain-list: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}

	err := StartWithContextAndInput(context.Background(), bytes.NewBufferString(""), &bytes.Buffer{}, fake)
	if err == nil || !strings.Contains(err.Error(), "domain-list") {
		t.Fatalf("start error = %v", err)
	}
	if fake.clearedPAC < 1 {
		t.Fatalf("cleanup before config validation calls = %d", fake.clearedPAC)
	}
	if fake.installedPAC != "" || fake.trusted != 0 {
		t.Fatalf("startup mutated OS state after invalid config: PAC=%q trusted=%d", fake.installedPAC, fake.trusted)
	}
}

func TestStartAllowsEmptyDomainListAndInstallsManagedPAC(t *testing.T) {
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
	if err := os.WriteFile(domainPath, []byte("# add domains when needed\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{}
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	go func() {
		done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), &out, fake)
	}()
	waitForStatusOutput(t, "domains: 0")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if fake.installedPAC == "" {
		t.Fatal("empty Domain List start did not install managed PAC")
	}
	if !strings.Contains(out.String(), "seamless-cors running") {
		t.Fatalf("start output = %q", out.String())
	}
}

func TestStartCleansPartialPACInstallFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := defaultTestConfig()
	cfg.DomainList = domainPath
	cfg.CATrusted = false

	configureGatewayForTest(t, cfg, "api.example.test\n")
	fake := &fakeAdapter{
		pacStates: []platform.PACServiceState{
			{Name: "Wi-Fi"},
			{Name: "USB LAN"},
		},
		installErr:       errors.New("install denied"),
		installFailAfter: 1,
	}

	err := StartWithContextAndInput(context.Background(), bytes.NewBufferString(""), io.Discard, fake)
	if err == nil || !strings.Contains(err.Error(), "install denied") {
		t.Fatalf("start error = %v", err)
	}
	if fake.clearedPAC == 0 {
		t.Fatal("partial PAC install failure did not run marker-based cleanup")
	}
	for _, state := range fake.pacStates {
		if state.Enabled && platform.IsManagedPACFootprint(state.URL) {
			t.Fatalf("partial PAC install cleanup left owned PAC enabled: %#v", state)
		}
	}
}

func TestStartDeclinedPACReplacementConsentDoesNotMutateOSOrRuntimeState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	domainPath := filepath.Join(configDir, "domains.txt")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, filepath.Join(configDir, "config.yaml"), testConfig{
		DomainList: domainPath,
		CATrusted:  true,
	})
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAdapter{
		pacStates: []platform.PACServiceState{{
			Name:    "Wi-Fi",
			URL:     "http://corp.example/proxy.pac",
			Enabled: true,
		}},
	}
	var out bytes.Buffer

	err := StartWithContextAndInput(context.Background(), bytes.NewBufferString("no"), &out, fake)
	if !errors.Is(err, ErrPACReplacementConsentDeclined) {
		t.Fatalf("start error = %v", err)
	}
	if fake.installedPAC != "" || fake.trusted != 1 {
		t.Fatalf("declined consent mutated OS state: PAC=%q trusted=%d", fake.installedPAC, fake.trusted)
	}
	if _, err := os.Stat(filepath.Join(configDir, "runtime", "gateway-state-cache.json")); !os.IsNotExist(err) {
		t.Fatalf("declined consent wrote runtime state: %v", err)
	}
	for _, want := range []string{"PAC Replacement Consent required", "without restoring previous PAC state"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("consent output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "CA Trust Consent:") {
		t.Fatalf("CA trust should not use PAC Replacement Consent prompt:\n%s", out.String())
	}
}

func TestManagedGatewayLeaseChecksSelectedServiceAfterReappearanceAtRefreshBoundary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := defaultTestConfig()
	cfg.DomainList = domainPath
	cfg.CATrusted = false

	configureGatewayForTest(t, cfg, "api.example.test\n")
	fake := &fakeAdapter{
		pacStates: []platform.PACServiceState{
			{Name: "Wi-Fi"},
			{Name: "USB LAN"},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	installedURL := waitForInstalledPAC(t, fake)

	fake.replacePACStates([]platform.PACServiceState{{
		Name:    "Wi-Fi",
		URL:     installedURL,
		Enabled: true,
	}})
	assertStillRunning(t, done)

	fake.appendPACState(platform.PACServiceState{
		Name:    "USB LAN",
		URL:     "http://user.example/proxy.pac",
		Enabled: true,
	})
	assertStillRunning(t, done)

	if err := os.WriteFile(domainPath, []byte("api-two.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, managedpac.ErrManagedPACLeaseLost) {
			t.Fatalf("serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime owner did not exit after PAC refresh found selected service with foreign PAC")
	}
}

func TestStartWithVerifiedActiveGatewaySkipsCleanupAndConfigValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := defaultTestConfig()
	firstAdapter := &fakeAdapter{}
	configureGatewayForTest(t, cfg, "api.example.test\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, firstAdapter) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	waitForStatusOutput(t, "seamless-cors status: running")

	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("domain-list: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secondAdapter := &fakeAdapter{}
	var out bytes.Buffer

	if err := StartWithContextAndInput(context.Background(), bytes.NewBufferString(""), &out, secondAdapter); err == nil || !strings.Contains(err.Error(), "gateway owner already running") {
		t.Fatalf("start error = %v", err)
	}
	if secondAdapter.clearedPAC != 0 || secondAdapter.cleanedCA != 0 {
		t.Fatalf("active second start ran cleanup: PAC=%d CA=%d", secondAdapter.clearedPAC, secondAdapter.cleanedCA)
	}
	if !strings.Contains(out.String(), "already running") {
		t.Fatalf("start output = %q", out.String())
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServeOwnerPublishesRouterOnlyOwnerWithoutManagedPAC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fake := &fakeAdapter{}
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	go func() { done <- ServeWithContext(ctx, &out, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	waitForStatusOutput(t, "gateway-runtime: inactive")
	if status := currentStatusOutput(t); strings.Contains(status, "cleanup-needed") {
		t.Fatalf("router-only owner reported live cache as cleanup-needed:\n%s", status)
	}

	if fake.installedPAC != "" || fake.clearedPAC != 0 || fake.trusted != 0 {
		t.Fatalf("serve mutated lifecycle state: PAC=%q cleared=%d trusted=%d", fake.installedPAC, fake.clearedPAC, fake.trusted)
	}
	if status := currentStatusOutput(t); !strings.Contains(status, "gateway-runtime: inactive") {
		t.Fatalf("router-only owner should not report Gateway Runtime as running:\n%s", status)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRouterHostedStartOutlivesHTTPStartRequest(t *testing.T) {
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
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeWithContext(ctx, io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	coord, err := gatewaycoord.Default()
	if err != nil {
		t.Fatal(err)
	}
	cache := coord.Verify().Cache
	result, err := gatewayclient.New(cache).Start(gatewayfacade.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != gatewayfacade.StartResultStarted {
		t.Fatalf("start result = %s", result.Kind)
	}
	waitForStatusOutput(t, "seamless-cors status: running")
	time.Sleep(100 * time.Millisecond)
	status := currentStatusOutput(t)
	if !strings.Contains(status, "seamless-cors status: running") {
		t.Fatalf("router-hosted start did not survive response:\n%s", status)
	}
	if strings.Contains(status, "cleanup-needed") {
		t.Fatalf("running owner reported live footprint as cleanup-needed:\n%s", status)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestStartWithRouterOnlyOwnerFailsWithoutDelegatingOrCleanup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ownerAdapter := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeWithContext(ctx, io.Discard, ownerAdapter) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	startAdapter := &fakeAdapter{}
	var out bytes.Buffer
	if err := StartWithContextAndInput(context.Background(), bytes.NewBufferString(""), &out, startAdapter); err == nil || !strings.Contains(err.Error(), "gateway owner already running") {
		t.Fatalf("start error = %v", err)
	}
	if !strings.Contains(out.String(), "gateway owner already running") {
		t.Fatalf("start output = %q", out.String())
	}
	if startAdapter.clearedPAC != 0 || startAdapter.installedPAC != "" {
		t.Fatalf("owner-blocked start mutated lifecycle state: cleared=%d PAC=%q", startAdapter.clearedPAC, startAdapter.installedPAC)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServeOwnerMayClaimStaleCacheWithoutGatewayFootprintCleanup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeStaleRuntimeState(t)
	fake := &fakeAdapter{
		pacStates: []platform.PACServiceState{{
			Name:    "Wi-Fi",
			URL:     "http://127.0.0.1:8079/seamless-cors.pac",
			Enabled: true,
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	go func() { done <- ServeWithContext(ctx, io.Discard, fake) }()
	waitForStatusOutput(t, "gateway-runtime: inactive")

	if fake.clearedPAC != 0 {
		t.Fatalf("serve cleaned PAC for stale cache: %d", fake.clearedPAC)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServeOwnerExitsWhenGatewayStateLeaseIsLost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeWithContext(ctx, io.Discard, &fakeAdapter{}) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	runtimeDir, err := liveconfig.RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	coord := gatewaycoord.New(runtimeDir)

	if err := coord.Remove(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve owner did not exit after lease loss")
	}
}

func TestRuntimeOwnerCleansUpWhenGatewayStateLeaseIsLost(t *testing.T) {
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
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	runtimeDir, err := liveconfig.RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	coord := gatewaycoord.New(runtimeDir)

	if err := coord.Remove(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime owner did not exit after lease loss")
	}
	if fake.clearedPAC == 0 {
		t.Fatal("runtime owner did not run Gateway Footprint Cleanup after lease loss")
	}
}

func TestStopReportsOwnerCleanupFailureAndKeepsOwnerForRetry(t *testing.T) {
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
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	fake.clearErr = errors.New("pac cleanup denied")
	var out bytes.Buffer
	err := stop(&out, fake)
	if err == nil || !strings.Contains(err.Error(), "pac cleanup denied") {
		t.Fatalf("stop error = %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("owner exited after failed cleanup: %v", err)
	default:
	}
	target, err := gatewayclient.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != gatewayclient.TargetActive {
		t.Fatal("owner was not reachable after failed cleanup")
	}

	fake.clearErr = nil
	out.Reset()
	if err := stop(&out, fake); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("owner did not exit after successful cleanup retry")
	}
}

func TestStatusAndStopDoNotBootstrapMissingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")

	var out bytes.Buffer
	if err := Status(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("status created config directory: %v", err)
	}
	if !strings.Contains(out.String(), "not running") {
		t.Fatalf("status output = %q", out.String())
	}

	out.Reset()
	if err := Stop(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("stop created config directory: %v", err)
	}
	if !strings.Contains(out.String(), "no running seamless-cors found") {
		t.Fatalf("stop output = %q", out.String())
	}
}

func TestStatusReportsStaleGatewayStateCacheWithoutCleanup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	statePath := writeStaleRuntimeState(t)

	var out bytes.Buffer
	if err := Status(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not running") {
		t.Fatalf("status output = %q", out.String())
	}
	if !strings.Contains(out.String(), "stale Gateway State Cache detected") {
		t.Fatalf("status output = %q", out.String())
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("status removed runtime state: %v", err)
	}
}

func TestStopCleansStaleGatewayStateCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	statePath := writeStaleRuntimeState(t)

	var out bytes.Buffer
	if err := Stop(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no running seamless-cors found") {
		t.Fatalf("stop output = %q", out.String())
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("stale Gateway State Cache still exists: %v", err)
	}
}

func TestManagedGatewayReloadsDomainListIntoGeneratedPAC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := defaultTestConfig()
	cfg.DomainList = domainPath

	configureGatewayForTest(t, cfg, "api-one.example.test\n")
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	pacURL := waitForInstalledPAC(t, fake)
	waitForHTTPBody(t, pacURL, "api-one.example.test")

	if err := os.WriteFile(domainPath, []byte("api-two.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForHTTPBody(t, pacURL, "api-two.example.test")
	refreshedURL := waitForRefreshedPAC(t, fake)
	if refreshedURL == fake.installedPAC {
		t.Fatalf("live reload reinstalled same PAC URL %q", refreshedURL)
	}
	if !strings.Contains(fake.installedPAC, "?v=1") || !strings.Contains(refreshedURL, "?v=2") {
		t.Fatalf("PAC URL versions: initial=%q refreshed=%q", fake.installedPAC, refreshedURL)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestManagedGatewayDoesNotRefreshPACForEquivalentDomainList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := defaultTestConfig()
	cfg.DomainList = domainPath

	configureGatewayForTest(t, cfg, "api.example.test\n")
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	waitForInstalledPAC(t, fake)

	if err := os.WriteFile(domainPath, []byte("# same route\nAPI.EXAMPLE.TEST\napi.example.test # duplicate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertNoPACRefresh(t, fake)

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestManagedGatewayStopsWhenManagedPACLeaseIsLostAndPreservesUserPAC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := defaultTestConfig()
	cfg.DomainList = domainPath

	configureGatewayForTest(t, cfg, "api.example.test\n")
	fake := &fakeAdapter{
		pacStates: []platform.PACServiceState{
			{Name: "Wi-Fi"},
			{Name: "USB LAN"},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	installedURL := waitForInstalledPAC(t, fake)

	fake.setPACState(0, platform.PACServiceState{Name: "Wi-Fi", URL: "http://user.example/proxy.pac", Enabled: true})
	fake.appendPACState(platform.PACServiceState{Name: "New Service", URL: installedURL, Enabled: true})
	assertStillRunning(t, done)

	if err := os.WriteFile(domainPath, []byte("api-two.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, managedpac.ErrManagedPACLeaseLost) {
			t.Fatalf("serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime owner did not exit after managed PAC lease loss")
	}

	if fake.pacStates[0].URL != "http://user.example/proxy.pac" || !fake.pacStates[0].Enabled {
		t.Fatalf("cleanup touched user PAC state: %#v", fake.pacStates[0])
	}
	if fake.pacStates[2].Enabled {
		t.Fatalf("marker-based cleanup did not clear owned new service: %#v", fake.pacStates[2])
	}
	if fake.pacStates[1].Enabled {
		t.Fatalf("cleanup did not clear still-owned managed service: %#v", fake.pacStates[1])
	}
	if fake.clearedPAC == 0 {
		t.Fatalf("marker-based PAC cleanup calls = %d", fake.clearedPAC)
	}
}

func TestStatusStopsManagedGatewayWhenManagedPACLeaseIsLost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := defaultTestConfig()

	configureGatewayForTest(t, cfg, "api.example.test\n")
	fake := &fakeAdapter{
		pacStates: []platform.PACServiceState{
			{Name: "Wi-Fi"},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	waitForInstalledPAC(t, fake)
	waitForStatusOutput(t, "seamless-cors status: running")

	fake.setPACState(0, platform.PACServiceState{Name: "Wi-Fi", URL: "http://user.example/proxy.pac", Enabled: true})
	assertStillRunning(t, done)

	var out bytes.Buffer
	err := Status(&out, &bytes.Buffer{})
	if !errors.Is(err, managedpac.ErrManagedPACLeaseLost) {
		t.Fatalf("status error = %v; output:\n%s", err, out.String())
	}

	select {
	case err := <-done:
		if !errors.Is(err, managedpac.ErrManagedPACLeaseLost) {
			t.Fatalf("serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime owner did not exit after status found managed PAC lease loss")
	}
}

func TestManagedGatewayCleansAttemptedPACURLAfterPartialRefreshFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := defaultTestConfig()
	cfg.DomainList = domainPath

	configureGatewayForTest(t, cfg, "api-one.example.test\n")
	fake := &fakeAdapter{
		pacStates: []platform.PACServiceState{
			{Name: "Wi-Fi"},
			{Name: "USB LAN"},
		},
		refreshErr:       errors.New("refresh denied"),
		refreshFailAfter: 1,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))
	initialURL := waitForInstalledPAC(t, fake)

	if err := os.WriteFile(domainPath, []byte("api-two.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "managed PAC refresh failed") {
			t.Fatalf("serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime owner did not exit after PAC refresh failure")
	}

	if len(fake.refreshedPAC) == 0 {
		t.Fatal("PAC refresh was not attempted")
	}
	attemptedURL := fake.refreshedPAC[len(fake.refreshedPAC)-1]
	if attemptedURL == initialURL {
		t.Fatalf("refresh attempted same PAC URL %q", attemptedURL)
	}
	for _, state := range fake.pacStates[:2] {
		if state.Enabled {
			t.Fatalf("cleanup left managed service enabled after partial refresh failure: %#v", state)
		}
	}
	if fake.clearedPAC == 0 {
		t.Fatalf("marker-based PAC cleanup calls = %d", fake.clearedPAC)
	}
}

func TestManagedGatewayStopsOnInvalidLiveDomainList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := defaultTestConfig()
	cfg.DomainList = domainPath

	configureGatewayForTest(t, cfg, "api-one.example.test\n")
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	pacURL := waitForInstalledPAC(t, fake)
	waitForHTTPBody(t, pacURL, "api-one.example.test")

	if err := os.WriteFile(domainPath, []byte("api-two.example.test\nhttps://*.bad.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "Fatal Domain List Error") {
			t.Fatalf("serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not stop after invalid live Domain List")
	}
	cancel()
}

func TestManagedGatewayAppliesEmptyLiveDomainList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	cfg := defaultTestConfig()
	cfg.DomainList = domainPath

	configureGatewayForTest(t, cfg, "api-one.example.test\n")
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	pacURL := waitForInstalledPAC(t, fake)
	waitForHTTPBody(t, pacURL, "api-one.example.test")

	if err := os.WriteFile(domainPath, []byte("# no active domains\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForStatusOutput(t, "domains: 0")
	body := fetchHTTPBody(t, pacURL)
	if strings.Contains(body, "api-one.example.test") {
		t.Fatalf("empty Domain List kept stale PAC route:\n%s", body)
	}
	if !strings.Contains(body, "return 'DIRECT'") {
		t.Fatalf("empty Domain List PAC should fall through to DIRECT:\n%s", body)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestManagedGatewayLiveConfigFollowsConfigDomainList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	secondDomainPath := filepath.Join(home, "second-domains.txt")
	configPath, err := liveconfig.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstDomainPath, []byte("first.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondDomainPath, []byte("second-one.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := defaultTestConfig()
	cfg.DomainList = firstDomainPath
	cfg.SourcePath = configPath
	cfg.CATrusted = false
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigForRuntime(t, configPath, cfg)

	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	pacURL := waitForInstalledPAC(t, fake)
	waitForHTTPBody(t, pacURL, "first.example.test")
	waitForStatusOutput(t, "domain-list: "+firstDomainPath)

	changed := defaultTestConfig()
	changed.DomainList = secondDomainPath
	changed.SourcePath = configPath
	changed.CATrusted = true
	writeConfigForRuntime(t, configPath, changed)
	waitForStatusOutput(t, "pending lifecycle changes: ca-trusted")
	if status := currentStatusOutput(t); !strings.Contains(status, "ca-trusted: false") {
		t.Fatalf("restart-applied lifecycle changed active CA trust:\n%s", status)
	}
	waitForStatusOutput(t, "domain-list: "+secondDomainPath)
	waitForHTTPBody(t, pacURL, "second-one.example.test")

	if err := os.WriteFile(secondDomainPath, []byte("second-two.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForHTTPBody(t, pacURL, "second-two.example.test")
	body := fetchHTTPBody(t, pacURL)
	if strings.Contains(body, "first.example.test") {
		t.Fatalf("old Domain List entry remained after config change:\n%s", body)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestManagedGatewayStopsOnInvalidLiveConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	domainPath := filepath.Join(home, "domains.txt")
	configPath, err := liveconfig.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(domainPath, []byte("api.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := defaultTestConfig()
	cfg.DomainList = domainPath
	cfg.SourcePath = configPath

	writeConfigForRuntime(t, configPath, cfg)
	fake := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartWithContextAndInput(ctx, bytes.NewBufferString(""), io.Discard, fake) }()
	waitForFile(t, filepath.Join(home, ".seamless-cors", "runtime", "gateway-state-cache.json"))

	if err := os.WriteFile(configPath, []byte("domain-list: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "domain-list") {
			t.Fatalf("serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not stop after invalid live config")
	}
}

func configureGatewayForTest(t *testing.T, cfg testConfig, domainText string) {
	t.Helper()
	if cfg.SourcePath == "" {
		configPath, err := liveconfig.DefaultConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		cfg.SourcePath = configPath
	}
	if cfg.DomainList == "" {
		cfg.DomainList = defaultTestConfig().DomainList
	}
	domainPath, err := liveconfig.ExpandPath(cfg.DomainList)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SourcePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(domainPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if domainText == "" {
		domainText = "# no active domains\n"
	}
	if err := os.WriteFile(domainPath, []byte(domainText), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.DomainList = domainPath
	writeConfigForRuntime(t, cfg.SourcePath, cfg)
}

func writeConfigForRuntime(t *testing.T, path string, cfg testConfig) {
	t.Helper()
	text := "domain-list: " + cfg.DomainList + "\n" +
		"ca-trusted: " + map[bool]string{true: "true", false: "false"}[cfg.CATrusted] + "\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func restoreAdapter(t *testing.T, adapter platform.Adapter) {
	t.Helper()
	previous := platform.CurrentAdapter
	platform.CurrentAdapter = adapter
	t.Cleanup(func() {
		platform.CurrentAdapter = previous
	})
}

func writeStaleRuntimeState(t *testing.T) string {
	t.Helper()
	runtimeDir, err := liveconfig.RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	coord := gatewaycoord.New(runtimeDir)
	err = coord.Write(gatewaycoord.GatewayStateCache{
		HTTPRouterListen: "127.0.0.1:1",
		Token:            "stale-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(runtimeDir, gatewaycoord.StateFileName)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitForInstalledPAC(t *testing.T, fake *fakeAdapter) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if installedPAC := fake.installedPACSnapshot(); installedPAC != "" {
			return installedPAC
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for PAC install")
	return ""
}

func waitForRefreshedPAC(t *testing.T, fake *fakeAdapter) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		refreshedPAC := fake.refreshedPACSnapshot()
		if len(refreshedPAC) > 0 {
			return refreshedPAC[len(refreshedPAC)-1]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for PAC refresh")
	return ""
}

func assertNoPACRefresh(t *testing.T, fake *fakeAdapter) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if refreshedPAC := fake.refreshedPACSnapshot(); len(refreshedPAC) > 0 {
			t.Fatalf("unexpected PAC refresh: %v", refreshedPAC)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertStillRunning(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("gateway exited early: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
}

func waitForHTTPBody(t *testing.T, url, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			last = string(body)
			if strings.Contains(last, want) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %s; last body:\n%s", want, url, last)
}

func fetchHTTPBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func waitForStatusOutput(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		var out bytes.Buffer
		if err := Status(&out, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		last = out.String()
		if strings.Contains(last, want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in status; last output:\n%s", want, last)
}

func currentStatusOutput(t *testing.T) string {
	t.Helper()
	var out bytes.Buffer
	if err := Status(&out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

package gateway

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/liveconfig"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

func TestPACVersionFollowsUpstreamListEntriesRevision(t *testing.T) {
	home := t.TempDir()
	firstUpstreamPath := filepath.Join(home, "first-upstreams.txt")
	secondUpstreamPath := filepath.Join(home, "second-upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeTrafficTestFile(t, firstUpstreamPath, "api.example.test\n")
	writeTrafficTestFile(t, secondUpstreamPath, "# same entries\nAPI.EXAMPLE.TEST\n")
	writeTrafficTestFile(t, configPath, "upstream-list: "+firstUpstreamPath+"\nca-trusted: false\n")

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	initial := source.Current()
	runtime, err := newRuntime(source, initial, trustedHTTPSAdmission{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, listener := range runtime.listeners {
			_ = listener.Close()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchLiveConfig(ctx, errs)
	initialPACURL := runtime.PACURL()

	writeTrafficTestFile(t, configPath, "upstream-list: "+secondUpstreamPath+"\nca-trusted: false\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return state.UpstreamList == secondUpstreamPath
	})
	if runtime.PACURL() != initialPACURL {
		t.Fatalf("path-only change advanced PAC URL from %q to %q", initialPACURL, runtime.PACURL())
	}

	writeTrafficTestFile(t, secondUpstreamPath, "API.EXAMPLE.TEST\nhttps://bad.example.test/path\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return len(state.UpstreamListWarnings) == 1
	})
	if runtime.PACURL() != initialPACURL {
		t.Fatalf("warning-only change advanced PAC URL from %q to %q", initialPACURL, runtime.PACURL())
	}

	writeTrafficTestFile(t, secondUpstreamPath, "changed.example.test\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return state.UpstreamCount == 1 && runtime.PACURL() != initialPACURL
	})
	if runtime.PACURL() == initialPACURL {
		t.Fatalf("Upstream List Entries Revision change did not advance PAC URL %q", initialPACURL)
	}
}

func TestLiveCATrustUsesOnlyAnAlreadyUsableAuthority(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeTrafficTestFile(t, upstreamPath, "api.example.test\n")
	writeTrafficTestFile(t, configPath, "upstream-list: "+upstreamPath+"\nca-trusted: false\n")

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	store := &trafficTestTrustStore{}
	authority, _, err := userca.Ensure(filepath.Join(home, "ca"), store)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newRuntime(source, source.Current(), trustedHTTPSAdmission{
		loadUsable: func(context.Context) (*userca.Authority, userca.Report, error) {
			return authority, userca.Report{Health: userca.HealthUsable}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetAuthority(nil); err != nil {
		t.Fatal(err)
	}

	initialPACURL := runtime.PACURL()
	writeTrafficTestFile(t, configPath, "upstream-list: "+upstreamPath+"\nca-trusted: true\n")
	enabled, err := source.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	runtime.applyLiveConfig(context.Background(), enabled)
	state := runtime.snapshot()
	if !state.CATrusted || !state.TrustedHTTPSActive || state.CATrustWarning != "" {
		t.Fatalf("enabled CA trust state = %#v", state)
	}
	if runtime.PACURL() == initialPACURL {
		t.Fatal("enabling trusted HTTPS did not advance the PAC URL")
	}

	writeTrafficTestFile(t, configPath, "upstream-list: "+upstreamPath+"\nca-trusted: false\n")
	disabled, err := source.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	runtime.applyLiveConfig(context.Background(), disabled)
	state = runtime.snapshot()
	if state.CATrusted || state.TrustedHTTPSActive || state.CATrustWarning != "" {
		t.Fatalf("disabled CA trust state = %#v", state)
	}
}

func TestLiveCATrustWarnsWhenUserCAIsNotUsable(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeTrafficTestFile(t, upstreamPath, "api.example.test\n")
	writeTrafficTestFile(t, configPath, "upstream-list: "+upstreamPath+"\nca-trusted: false\n")

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newRuntime(source, source.Current(), trustedHTTPSAdmission{
		loadUsable: func(context.Context) (*userca.Authority, userca.Report, error) {
			return nil, userca.Report{Health: userca.HealthMissing}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	if err := runtime.SetAuthority(nil); err != nil {
		t.Fatal(err)
	}

	initialPACURL := runtime.PACURL()
	writeTrafficTestFile(t, configPath, "upstream-list: "+upstreamPath+"\nca-trusted: true\n")
	configured, err := source.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	runtime.applyLiveConfig(context.Background(), configured)
	state := runtime.snapshot()
	if !state.CATrusted || state.TrustedHTTPSActive {
		t.Fatalf("unavailable CA trust state = %#v", state)
	}
	if !strings.Contains(state.CATrustWarning, "missing") {
		t.Fatalf("CA trust warning = %q", state.CATrustWarning)
	}
	if runtime.PACURL() != initialPACURL {
		t.Fatalf("inactive trusted HTTPS advanced PAC URL from %q to %q", initialPACURL, runtime.PACURL())
	}
}

type trafficTestTrustStore struct {
	records []userca.TrustedCertificate
}

func (s *trafficTestTrustStore) TrustedCertificates(context.Context) ([]userca.TrustedCertificate, error) {
	return append([]userca.TrustedCertificate(nil), s.records...), nil
}

func (s *trafficTestTrustStore) Trust(_ context.Context, certificatePEM []byte) error {
	fingerprint, err := userca.SHA1Fingerprint(certificatePEM)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certificatePEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	s.records = []userca.TrustedCertificate{{
		Fingerprint:    fingerprint,
		CertificatePEM: certificatePEM,
		ExpiresAt:      certificate.NotAfter,
	}}
	return nil
}

func (s *trafficTestTrustStore) Remove(context.Context, []string) error {
	s.records = nil
	return nil
}

func closeTrafficTestRuntime(runtime *trafficRuntime) {
	for _, listener := range runtime.listeners {
		_ = listener.Close()
	}
}

func writeTrafficTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForTrafficConfig(t *testing.T, runtime *trafficRuntime, errs <-chan serverError, ready func(runtimeState) bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready(runtime.snapshot()) {
			return
		}
		select {
		case err := <-errs:
			t.Fatal(err.err)
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("timed out waiting for Live Configuration update")
		}
	}
}

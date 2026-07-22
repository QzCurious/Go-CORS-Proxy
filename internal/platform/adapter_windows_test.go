//go:build windows

package platform

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

type fakeWindowsRunner struct {
	calls   []string
	out     []byte
	err     error
	runFunc func(context.Context, string, ...string) ([]byte, error)
}

func (f *fakeWindowsRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.runFunc != nil {
		return f.runFunc(ctx, name, args...)
	}
	return f.out, f.err
}

func TestWindowsCATrustMutationsObserveCancellation(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(context.Context, *WindowsAdapter) error
	}{
		{
			name: "trust",
			run: func(ctx context.Context, adapter *WindowsAdapter) error {
				return adapter.TrustCA(ctx, testWindowsCertificate(t, installedCACommonName, true))
			},
		},
		{
			name: "remove",
			run: func(ctx context.Context, adapter *WindowsAdapter) error {
				return adapter.RemoveCAs(ctx, []string{"ABCDEF"})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			entered := make(chan struct{})
			runner := &fakeWindowsRunner{runFunc: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
				close(entered)
				<-ctx.Done()
				return nil, ctx.Err()
			}}
			adapter := &WindowsAdapter{runner: runner}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- test.run(ctx, adapter) }()
			<-entered
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("mutation error = %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("CA mutation ignored cancellation")
			}
		})
	}
}

func TestWindowsAdapterInstallsPACForCurrentUser(t *testing.T) {
	runner := &fakeWindowsRunner{}
	notified := false
	adapter := &WindowsAdapter{runner: runner, notify: func() error {
		notified = true
		return nil
	}}

	installed, err := adapter.ApplyPAC("http://127.0.0.1:8079/seamless-cors.pac", []string{windowsPACServiceName})
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 || installed[0].ServiceName != windowsPACServiceName || installed[0].Outcome != PACApplyOutcomeApplied {
		t.Fatalf("installed services = %v", installed)
	}
	if !notified {
		t.Fatal("PAC install should notify WinINet settings changed")
	}
	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"powershell.exe",
		"New-ItemProperty",
		"AutoConfigURL",
		"http://127.0.0.1:8079/seamless-cors.pac",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing call fragment %q in:\n%s", want, joined)
		}
	}
}

func TestWindowsAdapterCurrentPACState(t *testing.T) {
	runner := &fakeWindowsRunner{out: []byte(`{"Name":"Windows Current User","URL":"http://old.example/proxy.pac","Enabled":true}`)}
	adapter := &WindowsAdapter{runner: runner}

	states, err := adapter.CurrentPACState()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %v", states)
	}
	state := states[0]
	if state.Name != windowsPACServiceName || state.URL != "http://old.example/proxy.pac" || !state.Enabled {
		t.Fatalf("state = %+v", state)
	}
}

func TestWindowsAdapterPreservesPACStateThatDoesNotMatchExpected(t *testing.T) {
	runner := &fakeWindowsRunner{out: []byte(`{"Name":"Windows Current User","URL":"http://corp.example/proxy.pac","Enabled":true}`)}
	adapter := &WindowsAdapter{runner: runner}

	expected := []PACServiceState{{Name: windowsPACServiceName, URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true}}
	if err := adapter.ClearPACIfMatches(expected); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(runner.calls, "\n"), "Remove-ItemProperty") {
		t.Fatalf("foreign PAC should not be cleared:\n%s", strings.Join(runner.calls, "\n"))
	}
}

func TestWindowsAdapterClearsMatchingPACState(t *testing.T) {
	runner := &fakeWindowsRunner{out: []byte(`{"Name":"Windows Current User","URL":"http://localhost:8079/seamless-cors.pac","Enabled":true}`)}
	notified := false
	adapter := &WindowsAdapter{runner: runner, notify: func() error {
		notified = true
		return nil
	}}

	expected := []PACServiceState{{Name: windowsPACServiceName, URL: "http://localhost:8079/seamless-cors.pac", Enabled: true}}
	if err := adapter.ClearPACIfMatches(expected); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "Remove-ItemProperty") {
		t.Fatalf("owned PAC should be cleared:\n%s", joined)
	}
	if !notified {
		t.Fatal("PAC cleanup should notify WinINet settings changed")
	}
}

func TestWindowsAdapterTrustsAndRemovesInstalledCAInUserRootStore(t *testing.T) {
	certPEM := testWindowsCertificate(t, installedCACommonName, true)
	runner := &fakeWindowsRunner{out: testWindowsCARecordsJSON(t, certPEM)}
	adapter := &WindowsAdapter{runner: runner}

	if err := adapter.TrustCA(context.Background(), certPEM); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RemoveCAs(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"Import-Certificate",
		"Cert:\\CurrentUser\\Root",
		"Remove-Item",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing call fragment %q in:\n%s", want, joined)
		}
	}
}

func TestWindowsCARecordsIgnoreSameNameNonCAFootprint(t *testing.T) {
	records, err := windowsCARecordsFromJSON(testWindowsCARecordsJSON(t, testWindowsCertificate(t, installedCACommonName, false)))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("non-CA cert with matching common name should be ignored: %v", records)
	}
}

func testWindowsCARecordsJSON(t *testing.T, certPEM []byte) []byte {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("test certificate PEM is invalid")
	}
	sum := sha1.Sum(block.Bytes)
	record := []struct {
		SHA1     string
		DER      string
		NotAfter string
	}{{
		SHA1:     strings.ToUpper(hex.EncodeToString(sum[:])),
		DER:      base64.StdEncoding.EncodeToString(block.Bytes),
		NotAfter: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
	}}
	out, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func testWindowsCertificate(t *testing.T, commonName string, isCA bool) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	usage := x509.KeyUsageDigitalSignature
	if isCA {
		usage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              usage,
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

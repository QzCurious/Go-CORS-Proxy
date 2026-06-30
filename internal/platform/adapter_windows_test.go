//go:build windows

package platform

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

type fakeWindowsRunner struct {
	calls []string
	out   []byte
	err   error
}

func (f *fakeWindowsRunner) run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return f.out, f.err
}

func TestWindowsAdapterInstallsPACForCurrentUser(t *testing.T) {
	runner := &fakeWindowsRunner{}
	notified := false
	adapter := &WindowsAdapter{runner: runner, notify: func() error {
		notified = true
		return nil
	}}

	installed, err := adapter.InstallPAC("http://127.0.0.1:8079/seamless-cors.pac", []string{windowsPACServiceName})
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 || installed[0] != windowsPACServiceName {
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

func TestWindowsAdapterClearsOnlyOwnedPACFootprint(t *testing.T) {
	runner := &fakeWindowsRunner{out: []byte(`{"Name":"Windows Current User","URL":"http://corp.example/proxy.pac","Enabled":true}`)}
	adapter := &WindowsAdapter{runner: runner}

	if err := adapter.ClearOwnedPAC(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(runner.calls, "\n"), "Remove-ItemProperty") {
		t.Fatalf("foreign PAC should not be cleared:\n%s", strings.Join(runner.calls, "\n"))
	}
}

func TestWindowsAdapterClearsOwnedPACFootprint(t *testing.T) {
	runner := &fakeWindowsRunner{out: []byte(`{"Name":"Windows Current User","URL":"http://localhost:8079/seamless-cors.pac","Enabled":true}`)}
	notified := false
	adapter := &WindowsAdapter{runner: runner, notify: func() error {
		notified = true
		return nil
	}}

	if err := adapter.ClearOwnedPAC(); err != nil {
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

	if err := adapter.TrustCA(certPEM); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RemoveCAs(nil); err != nil {
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

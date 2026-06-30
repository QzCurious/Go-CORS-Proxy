//go:build darwin

package platform

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls        []string
	err          error
	out          []byte
	autoProxyOut []byte
	findCertOut  []byte
}

func (f *fakeRunner) run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch args[0] {
	case "-listallnetworkservices":
		return []byte("An asterisk (*) denotes that a network service is disabled.\nWi-Fi\nThunderbolt Bridge\n"), nil
	case "-getautoproxyurl":
		if f.autoProxyOut != nil {
			return f.autoProxyOut, nil
		}
		return []byte("URL: http://old.example/proxy.pac\nEnabled: Yes\n"), nil
	case "find-certificate":
		if f.findCertOut != nil {
			return f.findCertOut, nil
		}
		return testFindCertificateOutput(nil), nil
	default:
		return f.out, f.err
	}
}

func TestDarwinAdapterInstallsPACWithoutRecordingPreviousAutoProxy(t *testing.T) {
	runner := &fakeRunner{}
	adapter := &DarwinAdapter{runner: runner}

	if _, err := adapter.InstallPAC("http://127.0.0.1:8079/seamless-cors.pac", []string{"Wi-Fi"}); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"networksetup -setautoproxyurl Wi-Fi http://127.0.0.1:8079/seamless-cors.pac",
		"networksetup -setautoproxystate Wi-Fi on",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing call %q in:\n%s", want, joined)
		}
	}
}

func TestDarwinAdapterClearsOnlyOwnedPACFootprints(t *testing.T) {
	runner := &fakeRunner{}
	adapter := &DarwinAdapter{runner: runner}

	if err := adapter.ClearOwnedPAC(); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.calls, "\n")
	if strings.Contains(joined, "-setautoproxystate Wi-Fi off") {
		t.Fatalf("foreign PAC should not be cleared:\n%s", joined)
	}
}

func TestDarwinAdapterClearsOwnedPACFootprintsAcrossServices(t *testing.T) {
	runner := &fakeRunner{
		autoProxyOut: []byte("URL: http://127.0.0.1:52144/nested/seamless-cors.pac\nEnabled: Yes\n"),
	}
	adapter := &DarwinAdapter{runner: runner}

	if err := adapter.ClearOwnedPAC(); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"networksetup -setautoproxystate Wi-Fi off",
		"networksetup -setautoproxystate Thunderbolt Bridge off",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing call %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "-setautoproxyurl") {
		t.Fatalf("cleanup should not attempt to blank PAC URLs:\n%s", joined)
	}
}

func TestDarwinAdapterMapsTrustApprovalCancellation(t *testing.T) {
	runner := &fakeRunner{
		out: []byte("SecTrustSettingsSetTrustSettings: The authorization was canceled by the user."),
		err: errors.New("exit status 1"),
	}
	adapter := &DarwinAdapter{runner: runner, keychainPath: "/tmp/login.keychain-db"}
	certPEM := []byte(`-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIBATAKBggqhkjOPQQDAjAUMRIwEAYDVQQDEwlkZXYtdGVz
dDAeFw0yNjAxMDEwMDAwMDBaFw0yNjAxMDIwMDAwMDBaMBQxEjAQBgNVBAMTCWRl
di10ZXN0MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEVm7vdnHl4ppQq91cHjbB
BiYUDhmY3ar6+P0gq0LrFs5vKiRbP+RlYoDx6K6P4iW22JwdAC3PeEGGhIOZLaNN
MEswDgYDVR0PAQH/BAQDAgEGMA8GA1UdEwEB/wQFMAMBAf8wKAYDVR0RBCEwH4IJ
ZGV2LXRlc3SHBH8AAAGHEAAAAAAAAAAAAAAAAAAAAAEwCgYIKoZIzj0EAwIDSAAw
RQIhAOoa4X7HjCOTEOEdPAQRxIhH3WETktsEOl3ZK9otm64jAiBEfd+WY1KcU6RC
3EpP1QovunMjInSJ/ksZrQPrLEpe7g==
-----END CERTIFICATE-----
`)

	if err := adapter.TrustCA(certPEM); !errors.Is(err, ErrTrustApprovalDenied) {
		t.Fatalf("trust error = %v", err)
	}
}

func TestDarwinAdapterTrustsAndRemovesInstalledCAInUserKeychain(t *testing.T) {
	runner := &fakeRunner{findCertOut: testFindCertificateOutput(testCertificate(t, installedCACommonName, true))}
	adapter := &DarwinAdapter{runner: runner, keychainPath: "/tmp/login.keychain-db"}
	certPEM := []byte(`-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIBATAKBggqhkjOPQQDAjAUMRIwEAYDVQQDEwlkZXYtdGVz
dDAeFw0yNjAxMDEwMDAwMDBaFw0yNjAxMDIwMDAwMDBaMBQxEjAQBgNVBAMTCWRl
di10ZXN0MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEVm7vdnHl4ppQq91cHjbB
BiYUDhmY3ar6+P0gq0LrFs5vKiRbP+RlYoDx6K6P4iW22JwdAC3PeEGGhIOZLaNN
MEswDgYDVR0PAQH/BAQDAgEGMA8GA1UdEwEB/wQFMAMBAf8wKAYDVR0RBCEwH4IJ
ZGV2LXRlc3SHBH8AAAGHEAAAAAAAAAAAAAAAAAAAAAEwCgYIKoZIzj0EAwIDSAAw
RQIhAOoa4X7HjCOTEOEdPAQRxIhH3WETktsEOl3ZK9otm64jAiBEfd+WY1KcU6RC
3EpP1QovunMjInSJ/ksZrQPrLEpe7g==
-----END CERTIFICATE-----
`)

	if err := adapter.TrustCA(certPEM); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RemoveCAs(nil); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"security add-trusted-cert -r trustRoot -p ssl -k /tmp/login.keychain-db",
		"security delete-certificate -Z",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing call %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "security add-trusted-cert -d") {
		t.Fatalf("CA trust should be installed in the current user's trust settings:\n%s", joined)
	}
}

func TestDarwinAdapterDoesNotRemoveSameNameNonCAFootprint(t *testing.T) {
	runner := &fakeRunner{findCertOut: testFindCertificateOutput(testCertificate(t, installedCACommonName, false))}
	adapter := &DarwinAdapter{runner: runner, keychainPath: "/tmp/login.keychain-db"}

	if err := adapter.RemoveCAs(nil); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.calls, "\n")
	if strings.Contains(joined, "security delete-certificate -Z") {
		t.Fatalf("non-CA cert with matching common name should not be deleted:\n%s", joined)
	}
}

func testFindCertificateOutput(certPEM []byte) []byte {
	if certPEM == nil {
		return []byte{}
	}
	return []byte("SHA-256 hash: 123456\nSHA-1 hash: ABCDEF123456\n" + string(certPEM))
}

func testCertificate(t *testing.T, commonName string, isCA bool) []byte {
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

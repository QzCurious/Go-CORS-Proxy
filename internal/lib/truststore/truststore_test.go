package truststore

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

type recordingPlatformStore struct {
	removeCalls        int
	removeFingerprints []string
}

func (*recordingPlatformStore) list(context.Context) ([]Certificate, error) { return nil, nil }
func (*recordingPlatformStore) add(context.Context, string) error           { return nil }
func (s *recordingPlatformStore) remove(_ context.Context, fingerprint string) error {
	s.removeCalls++
	s.removeFingerprints = append(s.removeFingerprints, fingerprint)
	return nil
}

func TestRemoveDelegatesToPlatformAdapter(t *testing.T) {
	platform := &recordingPlatformStore{}
	store := &Store{platform: platform}
	fingerprints := []string{" first ", "SECOND", "first"}

	if err := store.Remove(context.Background(), fingerprints); err != nil {
		t.Fatal(err)
	}
	if platform.removeCalls != 3 {
		t.Fatalf("remove calls = %d, want 3", platform.removeCalls)
	}
	want := fingerprints
	for i := range want {
		if platform.removeFingerprints[i] != want[i] {
			t.Fatalf("remove fingerprints = %#v, want %#v", platform.removeFingerprints, want)
		}
	}
}

func testCertificatePEM(t *testing.T, commonName string, isCA bool) []byte {
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

func mustParsePEM(t *testing.T, certificatePEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certificatePEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

package userca

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/truststore"
)

func TestInspectMissingIsNotUsable(t *testing.T) {
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: &fakeTrustStore{}, now: time.Now}
	current, err := ca.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Usable || current.SigningMaterial != nil {
		t.Fatal("missing UserCA exposed a usable capability")
	}
}

func TestInstallRecoversPartialAuthorityFootprints(t *testing.T) {
	fixture, err := createAuthority(filepath.Join(t.TempDir(), "fixture"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM, err := os.ReadFile(fixture.certPath)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*testing.T, string){
		"empty directory": func(t *testing.T, _ string) {},
		"certificate only": func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, certFileName), certificatePEM, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"private key only": func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, keyFileName), keyPEM, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, arrange := range tests {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "userca")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			arrange(t, dir)
			store := &fakeTrustStore{}
			ca := &CA{dir: dir, trustStore: store, now: time.Now}

			before, err := ca.Inspect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if before.Usable || before.SigningMaterial != nil {
				t.Fatal("partial UserCA exposed a usable capability")
			}

			after, err := ca.Install(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !after.Usable || after.SigningMaterial == nil || len(store.records) != 1 {
				t.Fatalf("recovered state = usable %t material %t trusted roots %d", after.Usable, after.SigningMaterial != nil, len(store.records))
			}
			if _, err := loadAuthority(dir); err != nil {
				t.Fatalf("recovered authority: %v", err)
			}
		})
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	store := &fakeTrustStore{}
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: store, now: time.Now}
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !first.Usable || first.SigningMaterial == nil {
		t.Fatalf("first install = usable %t material %t", first.Usable, first.SigningMaterial != nil)
	}
	firstAuthority, err := loadAuthority(ca.dir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondAuthority, err := loadAuthority(ca.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !secondAuthority.cert.Equal(firstAuthority.cert) {
		t.Fatal("idempotent install replaced the authority")
	}
}

func TestInstallRepairsTrustWithoutReplacingPair(t *testing.T) {
	store := &fakeTrustStore{}
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: store, now: time.Now}
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := loadAuthority(ca.dir)
	store.records = nil
	_, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, _ := loadAuthority(ca.dir)
	if !after.cert.Equal(before.cert) || len(store.records) != 1 {
		t.Fatal("trust repair did not reuse the valid local pair")
	}
}

func TestInstallVerifiesRepairedTrustBeforeReturningUsable(t *testing.T) {
	store := &fakeTrustStore{}
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: store, now: time.Now}
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.records = nil
	store.ignoreTrust = true

	if _, err := ca.Install(context.Background()); err == nil {
		t.Fatal("install reported usable after trust repair was not observable")
	}
}

func TestInstallRepairsPermissionsWithoutReplacingPair(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permission bits do not model the UserCA ACL")
	}
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: &fakeTrustStore{}, now: time.Now}
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := loadAuthority(ca.dir)
	if err := os.Chmod(before.keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, _ := loadAuthority(ca.dir)
	info, err := os.Stat(after.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !after.cert.Equal(before.cert) || info.Mode().Perm() != 0o600 {
		t.Fatal("permission repair did not preserve and secure the valid pair")
	}
}

func TestInspectReportsRenewalDueAndInstallReplacesWithoutOverlap(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeTrustStore{}
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: store, now: func() time.Time { return now }}
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := loadAuthority(ca.dir)
	now = first.ExpiresAt.Add(-renewalWindow)
	due, err := ca.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !due.Usable || !due.RenewalDue {
		t.Fatal("authority inside renewal window did not report renewal due")
	}
	renewed, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, _ := loadAuthority(ca.dir)
	if after.cert.Equal(before.cert) {
		t.Fatal("explicit renewal did not replace the authority")
	}
	if renewed.RenewalDue || len(store.records) != 1 {
		t.Fatalf("renewal = due %t trusted roots %d", renewed.RenewalDue, len(store.records))
	}
}

func TestInstallReplacesExpiredAuthority(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeTrustStore{}
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: store, now: func() time.Time { return now }}
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = first.ExpiresAt.Add(time.Second)
	expired, err := ca.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if expired.Usable {
		t.Fatal("expired authority reported usable")
	}
	replaced, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !replaced.Usable || len(store.records) != 1 {
		t.Fatal("expired authority was not cleanly replaced")
	}
}

func TestInstallClearsInvalidMaterialAndOwnedTrustBeforeReplacement(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "userca")
	store := &fakeTrustStore{}
	ca := &CA{dir: dir, trustStore: store, now: time.Now}
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, keyFileName), []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !current.Usable || len(store.records) != 1 {
		t.Fatal("invalid owned state was not replaced with one usable authority")
	}
}

func TestInstallReplacesAmbiguousOwnedTrust(t *testing.T) {
	store := &fakeTrustStore{}
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: store, now: time.Now}
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := loadAuthority(ca.dir)
	if err != nil {
		t.Fatal(err)
	}

	extra, err := createAuthority(filepath.Join(t.TempDir(), "extra"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(context.Background(), extra.certPath); err != nil {
		t.Fatal(err)
	}
	if len(store.records) != 2 {
		t.Fatalf("trusted roots = %d, want ambiguous pair", len(store.records))
	}
	current, err := ca.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Usable {
		t.Fatal("ambiguous owned trust reported usable")
	}

	replaced, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, err := loadAuthority(ca.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !replaced.Usable || len(store.records) != 1 || after.cert.Equal(before.cert) {
		t.Fatal("ambiguous owned trust was not replaced with one usable authority")
	}
}

func TestInstallDoesNotDestroyStateWhenInspectionCannotReadPair(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "userca")
	store := &fakeTrustStore{}
	ca := &CA{dir: dir, trustStore: store, now: time.Now}
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	recordsBefore := len(store.records)
	certPath := filepath.Join(dir, certFileName)
	originalReadFile := readFile
	readFile = func(path string) ([]byte, error) {
		if path == certPath {
			return nil, fs.ErrPermission
		}
		return originalReadFile(path)
	}
	t.Cleanup(func() { readFile = originalReadFile })
	if _, err := ca.Install(context.Background()); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("install error = %v, want read failure", err)
	}
	if len(store.records) != recordsBefore {
		t.Fatal("assessment failure triggered destructive trust cleanup")
	}
}

func TestInstallCleansLocalPairWhenTrustFails(t *testing.T) {
	trustErr := errors.New("trust installation failed")
	store := &fakeTrustStore{trustErr: trustErr}
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: store, now: time.Now}
	if _, err := ca.Install(context.Background()); !errors.Is(err, trustErr) {
		t.Fatalf("install error = %v, want trust installation failure", err)
	}
	if _, err := os.Stat(ca.dir); !os.IsNotExist(err) {
		t.Fatalf("failed install retained local material: %v", err)
	}
}

func TestUninstallRemovesEveryOwnedFactAndIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "userca")
	store := &fakeTrustStore{}
	ca := &CA{dir: dir, trustStore: store, now: time.Now}
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ca.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.records) != 0 {
		t.Fatal("uninstall did not remove every owned fact")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("UserCA storage remains: %v", err)
	}
	if err := ca.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallLeavesUnownedTrustUntouched(t *testing.T) {
	record := truststore.Certificate{
		Fingerprint: "UNRELATED",
		X509: &x509.Certificate{
			Subject:               pkix.Name{CommonName: ownedCACommonName},
			BasicConstraintsValid: true,
		},
	}
	store := &fakeTrustStore{records: []truststore.Certificate{record}}
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: store, now: time.Now}

	if err := ca.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.records) != 1 || store.records[0].Fingerprint != record.Fingerprint {
		t.Fatal("uninstall removed trust outside the seamless-cors ownership footprint")
	}
}

func TestUninstallReportsIncompleteTrustRemoval(t *testing.T) {
	store := &fakeTrustStore{}
	ca := &CA{dir: filepath.Join(t.TempDir(), "userca"), trustStore: store, now: time.Now}
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.removeErr = errors.New("remove denied")
	if err := ca.Uninstall(context.Background()); !errors.Is(err, store.removeErr) {
		t.Fatalf("uninstall error = %v, want removal error", err)
	}
}

type fakeTrustStore struct {
	records     []truststore.Certificate
	trustErr    error
	removeErr   error
	ignoreTrust bool
}

func (s *fakeTrustStore) List(context.Context) ([]truststore.Certificate, error) {
	return s.records, nil
}

func (s *fakeTrustStore) Add(_ context.Context, certificatePath string) error {
	if s.trustErr != nil {
		return s.trustErr
	}
	if s.ignoreTrust {
		return nil
	}
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		return err
	}
	added, err := testTrustStoreCertificate(certificatePEM)
	if err != nil {
		return err
	}
	for _, record := range s.records {
		if record.Fingerprint == added.Fingerprint {
			return nil
		}
	}
	s.records = append(s.records, added)
	return nil
}

func testTrustStoreCertificate(certificatePEM []byte) (truststore.Certificate, error) {
	block, _ := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return truststore.Certificate{}, fmt.Errorf("CA certificate PEM is invalid")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return truststore.Certificate{}, err
	}
	sum := sha1.Sum(block.Bytes)
	return truststore.Certificate{
		Fingerprint: strings.ToUpper(hex.EncodeToString(sum[:])),
		X509:        cert,
	}, nil
}

func (s *fakeTrustStore) Remove(_ context.Context, fingerprints []string) error {
	if s.removeErr != nil {
		return s.removeErr
	}
	remove := make(map[string]bool, len(fingerprints))
	for _, fingerprint := range fingerprints {
		remove[fingerprint] = true
	}
	kept := s.records[:0]
	for _, record := range s.records {
		if !remove[record.Fingerprint] {
			kept = append(kept, record)
		}
	}
	s.records = kept
	return nil
}

package userca

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestInspectMissingIsNotUsable(t *testing.T) {
	ca := openAt(filepath.Join(t.TempDir(), "userca"), &fakeTrustStore{}, time.Now)
	current, err := ca.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Usable || current.SigningMaterial() != nil {
		t.Fatal("missing UserCA exposed a usable capability")
	}
}

func TestInstallPublishesOnePairAndIsIdempotent(t *testing.T) {
	store := &fakeTrustStore{}
	ca := openAt(filepath.Join(t.TempDir(), "userca"), store, time.Now)
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed() || !first.Current().Usable || first.Current().SigningMaterial() == nil {
		t.Fatalf("first install = changed %t usable %t material %t", first.Changed(), first.Current().Usable, first.Current().SigningMaterial() != nil)
	}
	firstAuthority, err := loadAuthority(ca.dir)
	if err != nil {
		t.Fatal(err)
	}
	firstFingerprint, err := firstAuthority.fingerprint()
	if err != nil {
		t.Fatal(err)
	}

	second, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondAuthority, err := loadAuthority(ca.dir)
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint, err := secondAuthority.fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed() || secondFingerprint != firstFingerprint {
		t.Fatal("idempotent install replaced the authority")
	}
	if first.Current().SigningMaterial() == second.Current().SigningMaterial() {
		t.Fatal("fresh assessment reused a prior certificate value")
	}
	entries, err := os.ReadDir(ca.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("UserCA files = %d, want certificate and private key", len(entries))
	}
}

func TestInstallRepairsTrustWithoutReplacingPair(t *testing.T) {
	store := &fakeTrustStore{}
	ca := openAt(filepath.Join(t.TempDir(), "userca"), store, time.Now)
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := loadAuthority(ca.dir)
	wantFingerprint, _ := before.fingerprint()
	store.records = nil
	repaired, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, _ := loadAuthority(ca.dir)
	gotFingerprint, _ := after.fingerprint()
	if !repaired.Changed() || gotFingerprint != wantFingerprint || len(store.records) != 1 {
		t.Fatal("trust repair did not reuse the valid local pair")
	}
}

func TestInstallVerifiesRepairedTrustBeforeReturningUsable(t *testing.T) {
	store := &fakeTrustStore{}
	ca := openAt(filepath.Join(t.TempDir(), "userca"), store, time.Now)
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
	ca := openAt(filepath.Join(t.TempDir(), "userca"), &fakeTrustStore{}, time.Now)
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := loadAuthority(ca.dir)
	wantFingerprint, _ := before.fingerprint()
	if err := os.Chmod(before.keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	repaired, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, _ := loadAuthority(ca.dir)
	gotFingerprint, _ := after.fingerprint()
	info, err := os.Stat(after.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !repaired.Changed() || gotFingerprint != wantFingerprint || info.Mode().Perm() != 0o600 {
		t.Fatal("permission repair did not preserve and secure the valid pair")
	}
}

func TestInspectReportsRenewalDueAndInstallReplacesWithoutOverlap(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeTrustStore{}
	ca := openAt(filepath.Join(t.TempDir(), "userca"), store, func() time.Time { return now })
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := loadAuthority(ca.dir)
	firstFingerprint, _ := before.fingerprint()
	now = first.Current().ExpiresAt.Add(-renewalWindow)
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
	renewedFingerprint, _ := after.fingerprint()
	if !renewed.Changed() || renewedFingerprint == firstFingerprint {
		t.Fatal("explicit renewal did not replace the authority")
	}
	if renewed.Current().RenewalDue || len(store.records) != 1 {
		t.Fatalf("renewal = due %t trusted roots %d", renewed.Current().RenewalDue, len(store.records))
	}
}

func TestInstallReplacesExpiredAuthority(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeTrustStore{}
	ca := openAt(filepath.Join(t.TempDir(), "userca"), store, func() time.Time { return now })
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = first.Current().ExpiresAt.Add(time.Second)
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
	if !replaced.Changed() || !replaced.Current().Usable || len(store.records) != 1 {
		t.Fatal("expired authority was not cleanly replaced")
	}
}

func TestInstallClearsInvalidMaterialAndOwnedTrustBeforeReplacement(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "userca")
	store := &fakeTrustStore{}
	ca := openAt(dir, store, time.Now)
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, keyFileName), []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Current().Usable || len(store.records) != 1 {
		t.Fatal("invalid owned state was not replaced with one usable authority")
	}
}

func TestInstallDoesNotDestroyStateWhenInspectionCannotReadPair(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "userca")
	store := &fakeTrustStore{}
	ca := openAt(dir, store, time.Now)
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

func TestInstallCleansPublishedPairWhenTrustFails(t *testing.T) {
	store := &fakeTrustStore{trustErr: ErrApprovalDenied}
	ca := openAt(filepath.Join(t.TempDir(), "userca"), store, time.Now)
	if _, err := ca.Install(context.Background()); !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("install error = %v, want ErrApprovalDenied", err)
	}
	if _, err := os.Stat(ca.dir); !os.IsNotExist(err) {
		t.Fatalf("failed install retained local material: %v", err)
	}
}

func TestInstallReturnsErrorWhenFreshPostconditionCannotBeAssessed(t *testing.T) {
	wantErr := errors.New("trust assessment unavailable")
	store := &fakeTrustStore{trustedErrAt: 3, trustedErr: wantErr}
	ca := openAt(filepath.Join(t.TempDir(), "userca"), store, time.Now)
	if _, err := ca.Install(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("install error = %v, want final assessment error", err)
	}
}

func TestUninstallRemovesEveryOwnedFactAndIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "userca")
	store := &fakeTrustStore{}
	ca := openAt(dir, store, time.Now)
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := ca.Uninstall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed() || len(store.records) != 0 {
		t.Fatal("uninstall did not remove every owned fact")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("UserCA storage remains: %v", err)
	}
	second, err := ca.Uninstall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed() {
		t.Fatal("idempotent uninstall reported a change")
	}
}

func TestUninstallReportsIncompleteTrustRemoval(t *testing.T) {
	store := &fakeTrustStore{}
	ca := openAt(filepath.Join(t.TempDir(), "userca"), store, time.Now)
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.removeErr = errors.New("remove denied")
	if _, err := ca.Uninstall(context.Background()); !errors.Is(err, store.removeErr) {
		t.Fatalf("uninstall error = %v, want removal error", err)
	}
}

func TestNewCurrentStateRequiresSigningMaterial(t *testing.T) {
	if _, err := NewCurrentState(time.Now().Add(time.Hour), false, nil); err == nil {
		t.Fatal("usable current state accepted missing signing material")
	}
}

func TestCurrentStateRemainsStableWhileFreshInspectObservesExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ca := openAt(filepath.Join(t.TempDir(), "userca"), &fakeTrustStore{}, func() time.Time { return now })
	result, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	admitted := result.Current()
	now = admitted.ExpiresAt.Add(time.Second)
	if !admitted.Usable {
		t.Fatal("admitted current state changed as time advanced")
	}
	current, err := ca.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Usable {
		t.Fatal("fresh inspection did not observe expiry")
	}
}

type fakeTrustStore struct {
	records      []trustedCertificate
	trustErr     error
	removeErr    error
	trustedErrAt int
	trustedErr   error
	trustedCalls int
	ignoreTrust  bool
}

func (s *fakeTrustStore) TrustedCertificates(context.Context) ([]trustedCertificate, error) {
	s.trustedCalls++
	if s.trustedErrAt > 0 && s.trustedCalls >= s.trustedErrAt {
		return nil, s.trustedErr
	}
	return append([]trustedCertificate(nil), s.records...), nil
}

func (s *fakeTrustStore) Trust(_ context.Context, certificatePEM []byte) error {
	if s.trustErr != nil {
		return s.trustErr
	}
	if s.ignoreTrust {
		return nil
	}
	fingerprint, err := sha1Fingerprint(certificatePEM)
	if err != nil {
		return err
	}
	for _, record := range s.records {
		if record.Fingerprint == fingerprint {
			return nil
		}
	}
	s.records = append(s.records, trustedCertificate{Fingerprint: fingerprint, CertificatePEM: append([]byte(nil), certificatePEM...)})
	return nil
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

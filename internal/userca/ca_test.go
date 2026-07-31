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
	ca := openAt(t.TempDir(), &fakeTrustStore{}, time.Now)

	snapshot, err := ca.Inspect(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Usable() {
		t.Fatal("missing UserCA reported usable")
	}
	if _, ok := snapshot.TLSCertificate(); ok {
		t.Fatal("not-usable snapshot exposed TLS signing material")
	}
}

func TestInstallReturnsFreshUsableSnapshotAndIsIdempotent(t *testing.T) {
	store := &fakeTrustStore{}
	ca := openAt(t.TempDir(), store, time.Now)

	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed() || !first.Current().Usable() {
		t.Fatalf("first install = changed %t usable %t", first.Changed(), first.Current().Usable())
	}
	firstCertificate, ok := first.Current().TLSCertificate()
	if !ok {
		t.Fatal("installed snapshot omitted TLS material")
	}

	second, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed() {
		t.Fatal("idempotent install reported a user-visible change")
	}
	secondCertificate, ok := second.Current().TLSCertificate()
	if !ok || string(secondCertificate.Certificate[0]) != string(firstCertificate.Certificate[0]) {
		t.Fatal("idempotent install replaced the authority")
	}
}

func TestInstallRepairsMarkerIdentifiedAuthorityWithoutReplacingIt(t *testing.T) {
	store := &fakeTrustStore{}
	ca := openAt(t.TempDir(), store, time.Now)
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstCertificate, _ := first.Current().TLSCertificate()
	store.records = nil

	repaired, err := ca.Install(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if !repaired.Changed() || !repaired.Current().Usable() {
		t.Fatalf("repair = changed %t usable %t", repaired.Changed(), repaired.Current().Usable())
	}
	repairedCertificate, _ := repaired.Current().TLSCertificate()
	if string(repairedCertificate.Certificate[0]) != string(firstCertificate.Certificate[0]) {
		t.Fatal("repair replaced a valid marker-identified authority")
	}
}

func TestInstallReportsPermissionRepairAsChanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permission bits do not model the UserCA ACL")
	}
	dir := t.TempDir()
	ca := openAt(dir, &fakeTrustStore{}, time.Now)
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := readActiveFingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	active, err := loadGeneration(dir, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(active.keyPath, 0o644); err != nil {
		t.Fatal(err)
	}

	repaired, err := ca.Install(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if !repaired.Changed() {
		t.Fatal("permission repair was not reported as a user-visible change")
	}
	info, err := os.Stat(active.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("repaired key mode = %o", info.Mode().Perm())
	}
}

func TestInstallExplicitlyRenewsWhenDue(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store := &fakeTrustStore{}
	ca := openAt(t.TempDir(), store, clock)
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstCertificate, _ := first.Current().TLSCertificate()
	now = first.Current().ExpiresAt().Add(-renewalWindow).Add(time.Second)

	renewed, err := ca.Install(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if !renewed.Changed() || !renewed.Current().Usable() {
		t.Fatalf("renewal = changed %t usable %t", renewed.Changed(), renewed.Current().Usable())
	}
	renewedCertificate, _ := renewed.Current().TLSCertificate()
	if string(renewedCertificate.Certificate[0]) == string(firstCertificate.Certificate[0]) {
		t.Fatal("renewal reused the old authority")
	}
	if len(store.records) != 2 {
		t.Fatalf("trusted roots = %d, want previous root retained until runtime adoption", len(store.records))
	}
}

func TestInstallClearsAmbiguousResidueBeforeAddingCandidate(t *testing.T) {
	dir := t.TempDir()
	store := &fakeTrustStore{}
	ca := openAt(dir, store, time.Now)
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, activeFingerprintFileName), []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ca.Install(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if !result.Current().Usable() || len(store.records) != 1 {
		t.Fatalf("recovered install = usable %t trusted roots %d", result.Current().Usable(), len(store.records))
	}
}

func TestPostCommitCleanupFailureDoesNotHideUsableInstall(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeTrustStore{}
	ca := openAt(t.TempDir(), store, func() time.Time { return now })
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = first.Current().ExpiresAt().Add(-renewalWindow).Add(time.Second)
	store.removeErr = errors.New("remove denied")

	renewed, err := ca.Install(context.Background())

	if err != nil {
		t.Fatalf("committed renewal exposed private cleanup failure: %v", err)
	}
	if !renewed.Current().Usable() {
		t.Fatal("committed renewal did not return its usable postcondition")
	}
	if len(store.records) != 2 {
		t.Fatalf("trusted roots = %d, want retained old root plus Active root", len(store.records))
	}
}

func TestInstallWillNotAddAnotherCandidateUntilResidueCanBeCleared(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeTrustStore{}
	ca := openAt(t.TempDir(), store, func() time.Time { return now })
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = first.Current().ExpiresAt().Add(-renewalWindow).Add(time.Second)
	store.removeErr = errors.New("remove denied")
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(validity)
	trustedBefore := len(store.records)

	if _, err := ca.Install(context.Background()); !errors.Is(err, store.removeErr) {
		t.Fatalf("install error = %v, want cleanup precondition failure", err)
	}
	if len(store.records) != trustedBefore {
		t.Fatal("install added a root before ambiguous residue was cleared")
	}
}

func TestInstallVerifiesNonActiveCleanupBeforeAddingCandidate(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeTrustStore{}
	ca := openAt(t.TempDir(), store, func() time.Time { return now })
	first, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = first.Current().ExpiresAt().Add(-renewalWindow).Add(time.Second)
	second, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(store.records) != 2 {
		t.Fatalf("renewal roots = %d, want Active and Retired", len(store.records))
	}
	now = second.Current().ExpiresAt().Add(-renewalWindow).Add(time.Second)
	store.ignoreRemove = true

	if _, err := ca.Install(context.Background()); err == nil {
		t.Fatal("install added a Candidate without verifying non-active cleanup")
	}
	if len(store.records) != 2 {
		t.Fatalf("trusted roots = %d, Candidate was added before cleanup verification", len(store.records))
	}
}

func TestInstallReturnsAssessmentErrorForUnreadableActiveMarker(t *testing.T) {
	dir := t.TempDir()
	store := &fakeTrustStore{}
	ca := openAt(dir, store, time.Now)
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	recordsBefore := len(store.records)
	markerPath := filepath.Join(dir, activeFingerprintFileName)
	originalReadFile := readFile
	readFile = func(path string) ([]byte, error) {
		if path == markerPath {
			return nil, fs.ErrPermission
		}
		return originalReadFile(path)
	}
	t.Cleanup(func() { readFile = originalReadFile })

	if _, err := ca.Install(context.Background()); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("install error = %v, want marker assessment error", err)
	}
	if len(store.records) != recordsBefore {
		t.Fatal("assessment failure triggered destructive trust cleanup")
	}
}

func TestUninstallRemovesEveryOwnedFactAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	store := &fakeTrustStore{}
	ca := openAt(dir, store, time.Now)
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}

	first, err := ca.Uninstall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed() || first.Current().Usable() {
		t.Fatalf("first uninstall = changed %t usable %t", first.Changed(), first.Current().Usable())
	}
	if len(store.records) != 0 {
		t.Fatalf("trusted roots remain: %d", len(store.records))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("UserCA storage remains: %v", err)
	}

	second, err := ca.Uninstall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed() {
		t.Fatal("idempotent uninstall reported a user-visible change")
	}
}

func TestUninstallReportsIncompleteTrustRemoval(t *testing.T) {
	store := &fakeTrustStore{}
	ca := openAt(t.TempDir(), store, time.Now)
	if _, err := ca.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.removeErr = errors.New("remove denied")

	if _, err := ca.Uninstall(context.Background()); !errors.Is(err, store.removeErr) {
		t.Fatalf("uninstall error = %v, want removal error", err)
	}
}

func TestInstallPropagatesApprovalDenied(t *testing.T) {
	store := &fakeTrustStore{trustErr: ErrApprovalDenied}
	ca := openAt(t.TempDir(), store, time.Now)

	if _, err := ca.Install(context.Background()); !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("install error = %v, want ErrApprovalDenied", err)
	}
}

func TestInstallReturnsErrorWhenFreshPostconditionCannotBeAssessed(t *testing.T) {
	assessmentErr := errors.New("trust assessment unavailable")
	store := &fakeTrustStore{trustedErrAt: 3, trustedErr: assessmentErr}
	ca := openAt(t.TempDir(), store, time.Now)

	if _, err := ca.Install(context.Background()); !errors.Is(err, assessmentErr) {
		t.Fatalf("install error = %v, want final assessment error", err)
	}
}

func TestSnapshotReturnsDefensiveCertificateBytes(t *testing.T) {
	ca := openAt(t.TempDir(), &fakeTrustStore{}, time.Now)
	result, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, _ := result.Current().TLSCertificate()
	first.Certificate[0][0] ^= 0xff

	second, _ := result.Current().TLSCertificate()

	if first.Certificate[0][0] == second.Certificate[0][0] {
		t.Fatal("snapshot TLS material was mutated through a returned slice")
	}
}

func TestSnapshotRecordsInspectionTimeWhileLaterInspectUsesCurrentFacts(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ca := openAt(t.TempDir(), &fakeTrustStore{}, func() time.Time { return now })
	result, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	admitted := result.Current()
	now = admitted.ExpiresAt().Add(time.Second)

	if !admitted.Usable() {
		t.Fatal("immutable admitted snapshot changed as time advanced")
	}
	current, err := ca.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Usable() {
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
	ignoreRemove bool
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
	fingerprint, err := sha1Fingerprint(certificatePEM)
	if err != nil {
		return err
	}
	for _, record := range s.records {
		if record.Fingerprint == fingerprint {
			return nil
		}
	}
	s.records = append(s.records, trustedCertificate{
		Fingerprint:    fingerprint,
		CertificatePEM: append([]byte(nil), certificatePEM...),
	})
	return nil
}

func (s *fakeTrustStore) Remove(_ context.Context, fingerprints []string) error {
	if s.removeErr != nil {
		return s.removeErr
	}
	if s.ignoreRemove {
		return nil
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

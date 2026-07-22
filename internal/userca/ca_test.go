package userca

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeTrustStore struct {
	records []TrustedCertificate
	trusted int
	removed int
}

type crashingTrustStore struct {
	marker string
}

func (f crashingTrustStore) TrustedCertificates(context.Context) ([]TrustedCertificate, error) {
	return nil, nil
}
func (f crashingTrustStore) Remove(context.Context, []string) error { return nil }
func (f crashingTrustStore) Trust(context.Context, []byte) error {
	if err := os.WriteFile(f.marker, []byte("holding"), 0o600); err != nil {
		return err
	}
	select {}
}

func TestCAMutationLeaseIsReleasedWhenHolderProcessExits(t *testing.T) {
	if os.Getenv("SEAMLESS_CORS_CA_LEASE_HELPER") == "1" {
		dir := os.Getenv("SEAMLESS_CORS_CA_LEASE_DIR")
		marker := os.Getenv("SEAMLESS_CORS_CA_LEASE_MARKER")
		_, _, _ = Ensure(dir, crashingTrustStore{marker: marker})
		os.Exit(2)
	}

	base := t.TempDir()
	dir := filepath.Join(base, "ca")
	marker := filepath.Join(base, "holding")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCAMutationLeaseIsReleasedWhenHolderProcessExits$")
	cmd.Env = append(os.Environ(),
		"SEAMLESS_CORS_CA_LEASE_HELPER=1",
		"SEAMLESS_CORS_CA_LEASE_DIR="+dir,
		"SEAMLESS_CORS_CA_LEASE_MARKER="+marker,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("helper did not acquire CA mutation lease")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := EnsureContext(ctx, dir, &fakeTrustStore{}); err != nil {
		t.Fatalf("CA mutation lease remained held after process exit: %v", err)
	}
}

type blockingTrustStore struct {
	mu                    sync.Mutex
	records               []TrustedCertificate
	trustCalls            int
	trustEntered          chan struct{}
	releaseTrust          chan struct{}
	trustErr              error
	completeTrustOnCancel bool
}

func (f *blockingTrustStore) TrustedCertificates(context.Context) ([]TrustedCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TrustedCertificate(nil), f.records...), nil
}

func (f *blockingTrustStore) Trust(ctx context.Context, certPEM []byte) error {
	f.mu.Lock()
	f.trustCalls++
	f.mu.Unlock()
	select {
	case f.trustEntered <- struct{}{}:
	default:
	}
	select {
	case <-f.releaseTrust:
	case <-ctx.Done():
		if !f.completeTrustOnCancel {
			return ctx.Err()
		}
	}
	fingerprint, err := SHA1Fingerprint(certPEM)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.records = []TrustedCertificate{{Fingerprint: fingerprint, CertificatePEM: certPEM, ExpiresAt: cert.NotAfter}}
	err = f.trustErr
	f.mu.Unlock()
	return err
}

func TestCancelledEnsurePreservesCAWhenTrustInstallationCompleted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	store := &blockingTrustStore{
		trustEntered:          make(chan struct{}, 1),
		releaseTrust:          make(chan struct{}),
		completeTrustOnCancel: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct {
		authority *Authority
		result    EnsureResult
		err       error
	}, 1)
	go func() {
		authority, result, err := EnsureContext(ctx, dir, store)
		done <- struct {
			authority *Authority
			result    EnsureResult
			err       error
		}{authority: authority, result: result, err: err}
	}()
	select {
	case <-store.trustEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("CA Ensure did not reach trust installation")
	}
	cancel()
	got := <-done
	if got.err != nil {
		t.Fatalf("EnsureContext error = %v", got.err)
	}
	if got.authority == nil || got.result.Health != HealthUsable || !got.result.Changed {
		t.Fatalf("completed cancellation result = authority %v, result %+v", got.authority != nil, got.result)
	}
	report, err := Inspect(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	if report.Health != HealthUsable {
		t.Fatalf("health after completed trust installation = %s", report.Health)
	}
}

func TestCancelledEnsureReconcilesPartialCAStateToAbsent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	store := &blockingTrustStore{
		trustEntered: make(chan struct{}, 1),
		releaseTrust: make(chan struct{}),
		trustErr:     errors.New("approval command interrupted"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := EnsureContext(ctx, dir, store)
		done <- err
	}()
	select {
	case <-store.trustEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("CA Ensure did not reach trust installation")
	}
	cancel()
	close(store.releaseTrust)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureContext error = %v", err)
	}
	report, err := Inspect(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	if report.Health != HealthMissing {
		t.Fatalf("health after cancelled Ensure = %s", report.Health)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("partial local material remained: %v", err)
	}
}

func TestFailedEnsureRemovesPartialTrustAndMaterial(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	store := &blockingTrustStore{
		trustEntered: make(chan struct{}, 1),
		releaseTrust: make(chan struct{}),
		trustErr:     errors.New("trust command failed after changing state"),
	}
	close(store.releaseTrust)
	if _, _, err := Ensure(dir, store); err == nil {
		t.Fatal("Ensure should report failed trust command")
	}
	report, err := Inspect(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	if report.Health != HealthMissing {
		t.Fatalf("health after failed Ensure = %s", report.Health)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("partial local material remained: %v", err)
	}
}

func (f *blockingTrustStore) Remove(context.Context, []string) error {
	f.mu.Lock()
	f.records = nil
	f.mu.Unlock()
	return nil
}

func TestEnsureContextSerializesCAMutationAndWaitsCancellably(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	store := &blockingTrustStore{
		trustEntered: make(chan struct{}, 1),
		releaseTrust: make(chan struct{}),
	}
	firstDone := make(chan error, 1)
	go func() {
		_, _, err := EnsureContext(context.Background(), dir, store)
		firstDone <- err
	}()
	select {
	case <-store.trustEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("first CA mutation did not reach trust installation")
	}

	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		_, _, err := EnsureContext(ctx, dir, store)
		secondDone <- err
	}()
	cancel()
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting EnsureContext error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("waiting EnsureContext ignored cancellation")
	}
	store.mu.Lock()
	if got := store.trustCalls; got != 1 {
		store.mu.Unlock()
		t.Fatalf("trust calls while first mutation held lease = %d", got)
	}
	store.mu.Unlock()

	close(store.releaseTrust)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

type cancellationAwareRemoveStore struct {
	mu      sync.Mutex
	records []TrustedCertificate
	entered chan struct{}
}

func (f *cancellationAwareRemoveStore) TrustedCertificates(context.Context) ([]TrustedCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TrustedCertificate(nil), f.records...), nil
}

func (f *cancellationAwareRemoveStore) Trust(context.Context, []byte) error { return nil }

func (f *cancellationAwareRemoveStore) Remove(ctx context.Context, _ []string) error {
	select {
	case f.entered <- struct{}{}:
	default:
	}
	if ctx.Done() != nil {
		<-ctx.Done()
		return ctx.Err()
	}
	f.mu.Lock()
	f.records = nil
	f.mu.Unlock()
	return nil
}

func TestCancelledUninstallFinishesRemovingCAState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	initial := &fakeTrustStore{}
	if _, _, err := Ensure(dir, initial); err != nil {
		t.Fatal(err)
	}
	store := &cancellationAwareRemoveStore{
		records: append([]TrustedCertificate(nil), initial.records...),
		entered: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- UninstallContext(ctx, dir, store) }()
	select {
	case <-store.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("UserCA uninstall did not reach trust removal")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("UninstallContext error = %v", err)
	}
	report, err := Inspect(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	if report.Health != HealthMissing {
		t.Fatalf("health after cancelled uninstall = %s", report.Health)
	}
}

func (f *fakeTrustStore) TrustedCertificates(context.Context) ([]TrustedCertificate, error) {
	return append([]TrustedCertificate(nil), f.records...), nil
}

func (f *fakeTrustStore) Trust(_ context.Context, certPEM []byte) error {
	f.trusted++
	fingerprint, err := SHA1Fingerprint(certPEM)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	f.records = []TrustedCertificate{{
		Fingerprint:    fingerprint,
		CertificatePEM: certPEM,
		ExpiresAt:      cert.NotAfter,
	}}
	return nil
}

func (f *fakeTrustStore) Remove(context.Context, []string) error {
	f.removed++
	f.records = nil
	return nil
}

func TestEnsureInstallsMissingCA(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}

	authority, result, err := Ensure(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("missing CA should install fresh material")
	}
	if fake.trusted != 1 || fake.removed != 1 {
		t.Fatalf("trust lifecycle calls: trusted=%d removed=%d", fake.trusted, fake.removed)
	}
	for _, path := range []string{authority.CertPath, authority.KeyPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}

func TestEnsureReusesUsableCAAndRepairsPermissions(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}
	authority, _, err := Ensure(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(authority.KeyPath, 0o644); err != nil {
		t.Fatal(err)
	}

	reused, result, err := Ensure(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("usable CA should not be replaced")
	}
	if reused.CertPath != authority.CertPath {
		t.Fatalf("reused cert path = %q", reused.CertPath)
	}
	info, err := os.Stat(authority.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("key mode = %#o", got)
	}
}

func TestEnsureReplacesMismatchedMaterial(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}
	if _, _, err := Ensure(dir, fake); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, KeyFileName), []byte("bad key"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, result, err := Ensure(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("mismatched material should be replaced")
	}
	if fake.removed < 2 {
		t.Fatalf("remove calls = %d", fake.removed)
	}
}

func TestUninstallRemovesTrustAndLocalMaterial(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeTrustStore{}
	if _, _, err := Ensure(dir, fake); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(dir, fake); err != nil {
		t.Fatal(err)
	}
	if len(fake.records) != 0 {
		t.Fatalf("trusted records remained: %d", len(fake.records))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("CA dir remained: %v", err)
	}
}

func TestLoadUsableAdoptsCurrentTrustedAuthorityWithoutPrompting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	fake := &fakeTrustStore{}
	first, _, err := Ensure(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	firstFingerprint, err := first.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(dir, fake); err != nil {
		t.Fatal(err)
	}
	second, _, err := Ensure(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint, err := second.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint == secondFingerprint {
		t.Fatal("replacement should have a different CA identity")
	}
	trustCalls := fake.trusted

	loaded, report, err := LoadUsable(dir, fake)
	if err != nil {
		t.Fatal(err)
	}
	if report.Health != HealthUsable {
		t.Fatalf("loaded health = %s", report.Health)
	}
	loadedFingerprint, err := loaded.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if loadedFingerprint != secondFingerprint {
		t.Fatalf("loaded identity = %s, want current %s", loadedFingerprint, secondFingerprint)
	}
	if fake.trusted != trustCalls {
		t.Fatal("admission load must not prompt or install trust")
	}
}

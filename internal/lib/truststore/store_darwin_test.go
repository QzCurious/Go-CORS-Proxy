//go:build darwin

package truststore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeDarwinRunner struct {
	calls   []string
	out     []byte
	err     error
	runFunc func(context.Context, string, ...string) ([]byte, error)
}

func TestDarwinNewResolvesCurrentUserKeychain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := New()
	if err != nil {
		t.Fatal(err)
	}
	platform, ok := store.platform.(*darwinStore)
	if !ok {
		t.Fatalf("platform store = %T, want *darwinStore", store.platform)
	}
	want := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
	if platform.keychainPath != want {
		t.Fatalf("keychain path = %q, want %q", platform.keychainPath, want)
	}
}

func TestDarwinNewReturnsHomeResolutionError(t *testing.T) {
	t.Setenv("HOME", "")

	store, err := New()
	if err == nil {
		t.Fatalf("New() = %#v, nil; want home resolution error", store)
	}
	if store != nil {
		t.Fatalf("New() store = %#v, want nil", store)
	}
}

func (f *fakeDarwinRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.runFunc != nil {
		return f.runFunc(ctx, name, args...)
	}
	return f.out, f.err
}

func TestDarwinMutationsObserveCancellation(t *testing.T) {
	t.Run("add", func(t *testing.T) {
		assertDarwinMutationCancellation(t, func(ctx context.Context, store *Store) error {
			return store.Add(ctx, "/tmp/certificate.pem")
		}, nil)
	})
	t.Run("remove", func(t *testing.T) {
		certificatePEM := testCertificatePEM(t, "example", true)
		certificate := mustParsePEM(t, certificatePEM)
		assertDarwinMutationCancellation(t, func(ctx context.Context, store *Store) error {
			return store.Remove(ctx, []string{certificateFingerprint(certificate)})
		}, certificatePEM)
	})
}

func assertDarwinMutationCancellation(t *testing.T, mutate func(context.Context, *Store) error, listOut []byte) {
	t.Helper()
	entered := make(chan struct{})
	runner := &fakeDarwinRunner{runFunc: func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "find-certificate" {
			return listOut, nil
		}
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	store := testDarwinStore(runner)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mutate(ctx, store) }()
	<-entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("mutation error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("trust-store mutation ignored cancellation")
	}
}

func TestDarwinAddWrapsCommandFailureAndDiagnostic(t *testing.T) {
	commandErr := errors.New("exit status 1")
	runner := &fakeDarwinRunner{
		out: []byte("trust settings update failed"),
		err: commandErr,
	}
	store := testDarwinStore(runner)

	err := store.Add(context.Background(), "/tmp/certificate.pem")
	if !errors.Is(err, commandErr) {
		t.Fatalf("Add() error = %v, want wrapped command error", err)
	}
	if !strings.Contains(err.Error(), "trust settings update failed") {
		t.Fatalf("Add() error = %v, want command diagnostic", err)
	}
}

func TestDarwinAddUsesCurrentUserKeychain(t *testing.T) {
	runner := &fakeDarwinRunner{}
	store := testDarwinStore(runner)
	if err := store.Add(context.Background(), "/tmp/certificate.pem"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	want := "security add-trusted-cert -r trustRoot -p ssl -k /tmp/login.keychain-db /tmp/certificate.pem"
	if !strings.Contains(joined, want) {
		t.Fatalf("missing call %q in:\n%s", want, joined)
	}
	if strings.Contains(joined, "security add-trusted-cert -d") {
		t.Fatalf("trust should be added for the current user:\n%s", joined)
	}
}

func TestDarwinListReturnsEveryParseableCertificate(t *testing.T) {
	caPEM := testCertificatePEM(t, "owned-looking CA", true)
	leafPEM := testCertificatePEM(t, "unrelated leaf", false)
	runner := &fakeDarwinRunner{out: append(append([]byte("SHA-1 hash: ignored\n"), caPEM...), leafPEM...)}
	store := testDarwinStore(runner)

	certificates, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(certificates) != 2 {
		t.Fatalf("listed certificates = %d, want 2", len(certificates))
	}
	if joined := strings.Join(runner.calls, "\n"); strings.Contains(joined, " -c ") {
		t.Fatalf("List applied a product-specific certificate-name filter:\n%s", joined)
	}
	if certificates[0].X509.Subject.CommonName != "owned-looking CA" || certificates[1].X509.Subject.CommonName != "unrelated leaf" {
		t.Fatalf("listed certificates = %#v", certificates)
	}
	for _, certificate := range certificates {
		if len(certificate.Fingerprint) != 40 || certificate.Fingerprint != strings.ToUpper(certificate.Fingerprint) {
			t.Fatalf("fingerprint = %q, want normalized SHA-1", certificate.Fingerprint)
		}
	}
}

func TestDarwinListDoesNotTreatCommandFailureAsAnEmptyStore(t *testing.T) {
	commandErr := errors.New("exit status 44")
	runner := &fakeDarwinRunner{
		out: []byte("security: The specified item could not be found in the keychain."),
		err: commandErr,
	}
	store := testDarwinStore(runner)

	certificates, err := store.List(context.Background())
	if !errors.Is(err, commandErr) {
		t.Fatalf("List() error = %v, want wrapped command error", err)
	}
	if certificates != nil {
		t.Fatalf("List() certificates = %#v, want nil", certificates)
	}
}

func TestDarwinRemoveUsesFingerprintAndCurrentUserTrustSettings(t *testing.T) {
	certificatePEM := testCertificatePEM(t, "duplicate", true)
	fingerprint := certificateFingerprint(mustParsePEM(t, certificatePEM))
	deleteCalls := 0
	runner := &fakeDarwinRunner{runFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "delete-certificate" {
			deleteCalls++
		}
		return nil, nil
	}}
	store := testDarwinStore(runner)
	if err := store.Remove(context.Background(), []string{fingerprint}); err != nil {
		t.Fatal(err)
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", deleteCalls)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "delete-certificate -Z "+fingerprint+" -t /tmp/login.keychain-db") {
		t.Fatalf("removal did not include user trust settings:\n%s", joined)
	}
	if strings.Contains(joined, "find-certificate") {
		t.Fatalf("removal listed certificates:\n%s", joined)
	}
}

func TestDarwinRemoveReportsCommandFailure(t *testing.T) {
	certificatePEM := testCertificatePEM(t, "certificate", true)
	wantErr := errors.New("denied")
	runner := &fakeDarwinRunner{runFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "delete-certificate" {
			return []byte("authorization denied"), wantErr
		}
		return certificatePEM, nil
	}}
	store := testDarwinStore(runner)
	fingerprint := certificateFingerprint(mustParsePEM(t, certificatePEM))

	err := store.Remove(context.Background(), []string{fingerprint})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Remove() error = %v, want wrapped command error", err)
	}
}

func testDarwinStore(runner commandRunner) *Store {
	return &Store{platform: &darwinStore{runner: runner, keychainPath: "/tmp/login.keychain-db"}}
}

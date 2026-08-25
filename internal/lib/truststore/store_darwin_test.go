//go:build darwin

package truststore

import (
	"context"
	"errors"
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
		certificate := certificatesFromPEMOutput(certificatePEM)[0]
		assertDarwinMutationCancellation(t, func(ctx context.Context, store *Store) error {
			return store.Remove(ctx, []string{certificate.Fingerprint})
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
	store := &Store{runner: runner, keychainPath: "/tmp/login.keychain-db"}
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

func TestDarwinAddMapsApprovalCancellation(t *testing.T) {
	runner := &fakeDarwinRunner{
		out: []byte("SecTrustSettingsSetTrustSettings: The authorization was canceled by the user."),
		err: errors.New("exit status 1"),
	}
	store := &Store{runner: runner, keychainPath: "/tmp/login.keychain-db"}
	var denied *ApprovalDeniedError
	if err := store.Add(context.Background(), "/tmp/certificate.pem"); !errors.As(err, &denied) {
		t.Fatalf("add error = %v, want ApprovalDeniedError", err)
	}
}

func TestDarwinAddUsesCurrentUserKeychain(t *testing.T) {
	runner := &fakeDarwinRunner{}
	store := &Store{runner: runner, keychainPath: "/tmp/login.keychain-db"}
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
	store := &Store{runner: runner, keychainPath: "/tmp/login.keychain-db"}

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

func TestDarwinRemoveIsCompleteAndIdempotent(t *testing.T) {
	certificatePEM := testCertificatePEM(t, "duplicate", true)
	listed := append(append([]byte(nil), certificatePEM...), certificatePEM...)
	deleteCalls := 0
	runner := &fakeDarwinRunner{runFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch args[0] {
		case "find-certificate":
			return listed, nil
		case "delete-certificate":
			deleteCalls++
			listed = nil
			return nil, nil
		default:
			return nil, nil
		}
	}}
	store := &Store{runner: runner, keychainPath: "/tmp/login.keychain-db"}
	certificate := certificatesFromPEMOutput(certificatePEM)[0]
	if err := store.Remove(context.Background(), []string{certificate.Fingerprint, certificate.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want one per fingerprint", deleteCalls)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "delete-certificate -Z "+certificate.Fingerprint+" -t /tmp/login.keychain-db") {
		t.Fatalf("removal did not include user trust settings:\n%s", joined)
	}
	if err := store.Remove(context.Background(), []string{certificate.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	if deleteCalls != 1 {
		t.Fatal("idempotent removal attempted an absent fingerprint")
	}
}

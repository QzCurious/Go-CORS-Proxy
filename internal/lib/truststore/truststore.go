package truststore

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"os/exec"
	"strings"
)

// Certificate is one parseable certificate listed from the trust store.
// Fingerprint is uppercase SHA-1 hexadecimal without separators. Callers must
// treat X509 as read-only.
type Certificate struct {
	Fingerprint string
	X509        *x509.Certificate
}

// Store accesses the current user's operating-system trust store.
type Store struct {
	platform platformStore
}

type platformStore interface {
	list(ctx context.Context) ([]Certificate, error)
	add(ctx context.Context, certificatePath string) error
	remove(ctx context.Context, fingerprint string) error
}

// New resolves and returns the current user's operating-system trust store.
func New() (*Store, error) {
	platform, err := newPlatformStore()
	if err != nil {
		return nil, err
	}
	return &Store{platform: platform}, nil
}

// List returns a fresh, unordered snapshot containing one record for every
// parseable certificate in the current user's trust store.
func (s *Store) List(ctx context.Context) ([]Certificate, error) {
	return s.platform.list(ctx)
}

// Add adds a certificate file as a trusted root for the current user.
func (s *Store) Add(ctx context.Context, certificatePath string) error {
	return s.platform.add(ctx, certificatePath)
}

// Remove removes every trust-store entry matching any supplied fingerprint.
// Fingerprints use Certificate.Fingerprint format.
func (s *Store) Remove(ctx context.Context, fingerprints []string) error {
	var errs []error
	for _, fingerprint := range fingerprints {
		if err := s.platform.remove(ctx, fingerprint); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func certificateFingerprint(cert *x509.Certificate) string {
	sum := sha1.Sum(cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

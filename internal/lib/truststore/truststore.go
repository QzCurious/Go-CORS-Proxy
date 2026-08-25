package truststore

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
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
// Missing fingerprints are successful.
func (s *Store) Remove(ctx context.Context, fingerprints []string) error {
	targets := removalTargets(fingerprints)
	if len(targets) == 0 {
		return nil
	}
	before, err := s.List(ctx)
	if err != nil {
		return err
	}
	present := make(map[string]struct{}, len(targets))
	for _, certificate := range before {
		if _, ok := targets[certificate.Fingerprint]; ok {
			present[certificate.Fingerprint] = struct{}{}
		}
	}
	if len(present) == 0 {
		return nil
	}

	var errs []error
	for fingerprint := range present {
		if err := s.platform.remove(ctx, fingerprint); err != nil {
			errs = append(errs, err)
		}
	}
	after, err := s.List(ctx)
	if err != nil {
		errs = append(errs, err)
	} else if err := remainingRemovalError(after, targets); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ApprovalDeniedError reports that the current user declined an operating-
// system approval request while adding trust.
type ApprovalDeniedError struct {
	Cause error
}

func (e *ApprovalDeniedError) Error() string {
	if e.Cause == nil {
		return "trust approval denied"
	}
	return fmt.Sprintf("trust approval denied: %v", e.Cause)
}

func (e *ApprovalDeniedError) Unwrap() error { return e.Cause }

func certificateFingerprint(cert *x509.Certificate) string {
	sum := sha1.Sum(cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func removalTargets(fingerprints []string) map[string]struct{} {
	targets := make(map[string]struct{}, len(fingerprints))
	for _, fingerprint := range fingerprints {
		fingerprint = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(fingerprint), " ", ""))
		if fingerprint != "" {
			targets[fingerprint] = struct{}{}
		}
	}
	return targets
}

func remainingRemovalError(certificates []Certificate, targets map[string]struct{}) error {
	for _, certificate := range certificates {
		if _, ok := targets[certificate.Fingerprint]; ok {
			return fmt.Errorf("remove certificate %s: trust store still contains a matching entry", certificate.Fingerprint)
		}
	}
	return nil
}

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

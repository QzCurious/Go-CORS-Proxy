//go:build darwin

package truststore

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store accesses the current user's macOS login-keychain trust store.
type Store struct {
	runner       commandRunner
	keychainPath string
}

// New returns the current user's operating-system trust store.
func New() *Store {
	return &Store{runner: execRunner{}}
}

// List returns a fresh, unordered snapshot containing one record for every
// parseable certificate in the current user's trust store.
func (s *Store) List(ctx context.Context) ([]Certificate, error) {
	out, err := s.security(ctx, "find-certificate", "-a", "-p", "-Z", s.keychain())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "could not be found") ||
			strings.Contains(strings.ToLower(string(out)), "could not be found") {
			return nil, nil
		}
		return nil, err
	}
	return certificatesFromPEMOutput(out), nil
}

// Add adds a certificate file as a trusted SSL root for the current user.
func (s *Store) Add(ctx context.Context, certificatePath string) error {
	_, err := s.security(ctx, "add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k", s.keychain(), certificatePath)
	if isTrustApprovalDenied(err) {
		return &ApprovalDeniedError{Cause: err}
	}
	return err
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
		if _, err := s.security(ctx, "delete-certificate", "-Z", fingerprint, "-t", s.keychain()); err != nil {
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

func certificatesFromPEMOutput(out []byte) []Certificate {
	var certificates []Certificate
	for len(out) > 0 {
		block, rest := pem.Decode(out)
		if block == nil {
			break
		}
		out = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		certificates = append(certificates, Certificate{Fingerprint: certificateFingerprint(cert), X509: cert})
	}
	return certificates
}

func (s *Store) security(ctx context.Context, args ...string) ([]byte, error) {
	out, err := s.runner.run(ctx, "security", args...)
	if err != nil {
		return out, fmt.Errorf("security %s failed: %s: %w", strings.Join(args, " "), bytes.TrimSpace(out), err)
	}
	return out, nil
}

func (s *Store) keychain() string {
	if s.keychainPath != "" {
		return s.keychainPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "login.keychain-db"
	}
	return filepath.Join(home, "Library", "Keychains", "login.keychain-db")
}

func isTrustApprovalDenied(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "authorization was canceled") ||
		strings.Contains(text, "authorization was cancelled") ||
		strings.Contains(text, "authorization has been denied") ||
		strings.Contains(text, "user canceled") ||
		strings.Contains(text, "user cancelled")
}

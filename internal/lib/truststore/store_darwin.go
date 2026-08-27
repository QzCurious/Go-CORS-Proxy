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
)

type store struct {
	runner       commandRunner
	keychainPath string
}

var _ Store = (*store)(nil)

// New resolves and returns the current user's operating-system trust store.
func New() (Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve current user home directory: %w", err)
	}
	return &store{
		runner:       execRunner{},
		keychainPath: filepath.Join(home, "Library", "Keychains", "login.keychain-db"),
	}, nil
}

// List returns a fresh, unordered snapshot containing one record for every
// parseable certificate in the current user's trust store.
func (s *store) List(ctx context.Context) ([]Certificate, error) {
	out, err := s.runner.run(ctx, "security", "find-certificate", "-a", "-p", s.keychainPath)
	if err != nil {
		return nil, fmt.Errorf("list trusted certificates: %s: %w", bytes.TrimSpace(out), err)
	}

	// Decode the PEM stream emitted by security.
	var encodedCertificates [][]byte
	for len(out) > 0 {
		block, rest := pem.Decode(out)
		if block == nil {
			break
		}
		out = rest
		if block.Type == "CERTIFICATE" {
			encodedCertificates = append(encodedCertificates, block.Bytes)
		}
	}

	// Parse each certificate independently so one malformed entry does not hide the rest.
	certificates := make([]Certificate, 0, len(encodedCertificates))
	for _, encoded := range encodedCertificates {
		certificate, err := x509.ParseCertificate(encoded)
		if err != nil {
			continue
		}
		certificates = append(certificates, Certificate{
			Fingerprint: certificateFingerprint(certificate),
			X509:        certificate,
		})
	}
	return certificates, nil
}

// Add adds a certificate file as a trusted root for the current user.
func (s *store) Add(ctx context.Context, certificatePath string) error {
	out, err := s.runner.run(ctx, "security", "add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k", s.keychainPath, certificatePath)
	if err != nil {
		return fmt.Errorf("add trusted certificate: %s: %w", bytes.TrimSpace(out), err)
	}
	return nil
}

// Remove removes every trust-store entry matching any supplied fingerprint.
// Fingerprints use Certificate.Fingerprint format.
func (s *store) Remove(ctx context.Context, fingerprints []string) error {
	var errs []error
	for _, fingerprint := range fingerprints {
		out, err := s.runner.run(ctx, "security", "delete-certificate", "-Z", fingerprint, "-t", s.keychainPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("remove trusted certificate %s: %s: %w", fingerprint, bytes.TrimSpace(out), err))
		}
	}
	return errors.Join(errs...)
}

//go:build darwin

package truststore

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

type darwinStore struct {
	runner       commandRunner
	keychainPath string
}

var _ platformStore = (*darwinStore)(nil)

func newPlatformStore() (platformStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve current user home directory: %w", err)
	}
	return &darwinStore{
		runner:       execRunner{},
		keychainPath: filepath.Join(home, "Library", "Keychains", "login.keychain-db"),
	}, nil
}

func (s *darwinStore) list(ctx context.Context) ([]Certificate, error) {
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

func (s *darwinStore) add(ctx context.Context, certificatePath string) error {
	out, err := s.runner.run(ctx, "security", "add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k", s.keychainPath, certificatePath)
	if err != nil {
		return fmt.Errorf("add trusted certificate: %s: %w", bytes.TrimSpace(out), err)
	}
	return nil
}

func (s *darwinStore) remove(ctx context.Context, fingerprint string) error {
	out, err := s.runner.run(ctx, "security", "delete-certificate", "-Z", fingerprint, "-t", s.keychainPath)
	if err != nil {
		return fmt.Errorf("remove trusted certificate %s: %s: %w", fingerprint, bytes.TrimSpace(out), err)
	}
	return nil
}

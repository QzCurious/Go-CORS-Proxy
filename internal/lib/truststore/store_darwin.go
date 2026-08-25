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
	"strings"
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
	out, err := s.runner.run(ctx, "security", "find-certificate", "-a", "-p", "-Z", s.keychainPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "could not be found") ||
			strings.Contains(strings.ToLower(string(out)), "could not be found") {
			return nil, nil
		}
		return nil, fmt.Errorf("list trusted certificates: %s: %w", bytes.TrimSpace(out), err)
	}
	return certificatesFromPEMOutput(out), nil
}

func (s *darwinStore) add(ctx context.Context, certificatePath string) error {
	out, err := s.runner.run(ctx, "security", "add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k", s.keychainPath, certificatePath)
	if err == nil {
		return nil
	}
	failure := fmt.Errorf("add trusted certificate: %s: %w", bytes.TrimSpace(out), err)
	if isTrustApprovalDenied(out, err) {
		return &ApprovalDeniedError{Cause: failure}
	}
	return failure
}

func (s *darwinStore) remove(ctx context.Context, fingerprint string) error {
	out, err := s.runner.run(ctx, "security", "delete-certificate", "-Z", fingerprint, "-t", s.keychainPath)
	if err != nil {
		return fmt.Errorf("remove trusted certificate %s: %s: %w", fingerprint, bytes.TrimSpace(out), err)
	}
	return nil
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

func isTrustApprovalDenied(out []byte, err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(text, "authorization was canceled") ||
		strings.Contains(text, "authorization was cancelled") ||
		strings.Contains(text, "authorization has been denied") ||
		strings.Contains(text, "user canceled") ||
		strings.Contains(text, "user cancelled")
}

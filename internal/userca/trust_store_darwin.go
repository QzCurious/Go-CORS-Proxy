//go:build darwin

package userca

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type darwinTrustStore struct {
	runner       commandRunner
	keychainPath string
}

func newTrustStore() trustStore {
	return &darwinTrustStore{runner: execRunner{}}
}

var _ trustStore = (*darwinTrustStore)(nil)

func (s *darwinTrustStore) Trust(ctx context.Context, certificatePEM []byte) error {
	block, _ := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return fmt.Errorf("CA certificate PEM is invalid")
	}
	dir, err := os.MkdirTemp("", "seamless-cors-ca-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	certificatePath := filepath.Join(dir, "root-ca.pem")
	if err := os.WriteFile(certificatePath, certificatePEM, 0o600); err != nil {
		return err
	}
	_, err = s.security(ctx, "add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k", s.keychain(), certificatePath)
	if isTrustApprovalDenied(err) {
		return fmt.Errorf("%w: %w", ErrApprovalDenied, err)
	}
	return err
}

func (s *darwinTrustStore) TrustedCertificates(ctx context.Context) ([]trustedCertificate, error) {
	out, err := s.security(ctx, "find-certificate", "-a", "-c", ownedCACommonName, "-p", "-Z", s.keychain())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "could not be found") ||
			strings.Contains(strings.ToLower(string(out)), "could not be found") {
			return nil, nil
		}
		return nil, err
	}
	return strictFootprintCertificates(out), nil
}

func (s *darwinTrustStore) Remove(ctx context.Context, fingerprints []string) error {
	var firstErr error
	for _, fingerprint := range fingerprints {
		if _, err := s.security(ctx, "delete-certificate", "-Z", fingerprint, s.keychain()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func strictFootprintCertificates(out []byte) []trustedCertificate {
	var certificates []trustedCertificate
	var fingerprint string
	var pemLines []string
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(trimmed, "SHA-1 hash: "); ok {
			if certificate, ok := trustedCertificateFromPEM(fingerprint, pemLines); ok {
				certificates = append(certificates, certificate)
			}
			fingerprint = strings.TrimSpace(value)
			pemLines = nil
		} else if fingerprint != "" {
			pemLines = append(pemLines, line)
		}
	}
	if certificate, ok := trustedCertificateFromPEM(fingerprint, pemLines); ok {
		certificates = append(certificates, certificate)
	}
	return certificates
}

func trustedCertificateFromPEM(fingerprint string, pemLines []string) (trustedCertificate, bool) {
	if fingerprint == "" || len(pemLines) == 0 {
		return trustedCertificate{}, false
	}
	certificatePEM := []byte(strings.Join(pemLines, "\n"))
	block, _ := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return trustedCertificate{}, false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !isStrictFootprint(cert) {
		return trustedCertificate{}, false
	}
	return trustedCertificate{
		Fingerprint:    fingerprint,
		CertificatePEM: certificatePEM,
		ExpiresAt:      cert.NotAfter,
	}, true
}

func (s *darwinTrustStore) security(ctx context.Context, args ...string) ([]byte, error) {
	out, err := s.runner.run(ctx, "security", args...)
	if err != nil {
		return out, fmt.Errorf("security %s failed: %s: %w", strings.Join(args, " "), bytes.TrimSpace(out), err)
	}
	return out, nil
}

func (s *darwinTrustStore) keychain() string {
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

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

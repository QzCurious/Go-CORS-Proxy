//go:build windows

package userca

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type windowsTrustStore struct {
	runner commandRunner
}

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func newTrustStore() TrustStore {
	return &windowsTrustStore{runner: execRunner{}}
}

var _ TrustStore = (*windowsTrustStore)(nil)

func (s *windowsTrustStore) Trust(ctx context.Context, certificatePEM []byte) error {
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
	script := fmt.Sprintf(`
Import-Certificate -FilePath %s -CertStoreLocation Cert:\CurrentUser\Root | Out-Null
`, psQuote(certificatePath))
	_, err = s.powershell(ctx, script)
	return err
}

func (s *windowsTrustStore) TrustedCertificates(ctx context.Context) ([]TrustedCertificate, error) {
	script := fmt.Sprintf(`
$records = @(
	Get-ChildItem -Path Cert:\CurrentUser\Root |
		Where-Object { $_.Subject -eq %s } |
		ForEach-Object {
			[pscustomobject]@{
				Fingerprint = $_.Thumbprint.ToUpperInvariant()
				DER = [Convert]::ToBase64String($_.RawData)
				ExpiresAt = $_.NotAfter.ToUniversalTime().ToString('o')
			}
		}
)
ConvertTo-Json -Compress -InputObject $records
`, psQuote("CN="+CommonName))
	out, err := s.powershell(ctx, script)
	if err != nil {
		return nil, err
	}
	return windowsTrustedCertificatesFromJSON(out)
}

func (s *windowsTrustStore) Remove(ctx context.Context, fingerprints []string) error {
	var firstErr error
	for _, fingerprint := range fingerprints {
		script := fmt.Sprintf(`
Remove-Item -Path %s -ErrorAction SilentlyContinue
`, psQuote(`Cert:\CurrentUser\Root\`+fingerprint))
		if _, err := s.powershell(ctx, script); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *windowsTrustStore) powershell(ctx context.Context, script string) ([]byte, error) {
	out, err := s.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return out, fmt.Errorf("powershell failed: %s: %w", bytes.TrimSpace(out), err)
	}
	return out, nil
}

func windowsTrustedCertificatesFromJSON(out []byte) ([]TrustedCertificate, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	type rawTrustedCertificate struct {
		Fingerprint string
		DER         string
		ExpiresAt   string
	}
	var raw []rawTrustedCertificate
	if bytes.HasPrefix(trimmed, []byte("{")) {
		var single rawTrustedCertificate
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return nil, fmt.Errorf("parse Windows trusted certificate: %w", err)
		}
		raw = []rawTrustedCertificate{single}
	} else if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("parse Windows trusted certificates: %w", err)
	}
	certificates := make([]TrustedCertificate, 0, len(raw))
	for _, item := range raw {
		der, err := base64.StdEncoding.DecodeString(item.DER)
		if err != nil {
			return nil, fmt.Errorf("decode Windows trusted certificate: %w", err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("parse Windows trusted certificate: %w", err)
		}
		if !isStrictFootprint(cert) {
			continue
		}
		expiresAt, err := parseExpiresAt(item.ExpiresAt, cert.NotAfter)
		if err != nil {
			return nil, err
		}
		fingerprint := strings.ToUpper(strings.ReplaceAll(item.Fingerprint, " ", ""))
		if fingerprint == "" {
			sum := sha1.Sum(der)
			fingerprint = strings.ToUpper(hex.EncodeToString(sum[:]))
		}
		certificates = append(certificates, TrustedCertificate{
			Fingerprint:    fingerprint,
			CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			ExpiresAt:      expiresAt,
		})
	}
	return certificates, nil
}

func parseExpiresAt(raw string, fallback time.Time) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse Windows certificate expiration: %w", err)
	}
	return value, nil
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

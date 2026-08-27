//go:build windows

package truststore

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

type store struct {
	runner commandRunner
}

var _ Store = (*store)(nil)

// New resolves and returns the current user's operating-system trust store.
func New() (Store, error) {
	return &store{runner: execRunner{}}, nil
}

// List returns a fresh, unordered snapshot containing one record for every
// parseable certificate in the current user's trust store.
func (s *store) List(ctx context.Context) ([]Certificate, error) {
	script := `
$records = @(
	Get-ChildItem -Path Cert:\CurrentUser\Root |
		ForEach-Object {
			[pscustomobject]@{
				DER = [Convert]::ToBase64String($_.RawData)
			}
		}
)
ConvertTo-Json -Compress -InputObject $records
`
	out, err := s.powershell(ctx, script)
	if err != nil {
		return nil, err
	}
	return certificatesFromJSON(out)
}

// Add adds a certificate file as a trusted root for the current user.
func (s *store) Add(ctx context.Context, certificatePath string) error {
	script := fmt.Sprintf(`
Import-Certificate -FilePath %s -CertStoreLocation Cert:\CurrentUser\Root -ErrorAction Stop | Out-Null
`, psQuote(certificatePath))
	_, err := s.powershell(ctx, script)
	return err
}

// Remove removes every trust-store entry matching any supplied fingerprint.
// Fingerprints use Certificate.Fingerprint format.
func (s *store) Remove(ctx context.Context, fingerprints []string) error {
	if len(fingerprints) == 0 {
		return nil
	}

	quotedFingerprints := make([]string, len(fingerprints))
	for i, fingerprint := range fingerprints {
		quotedFingerprints[i] = psQuote(fingerprint)
	}
	script := fmt.Sprintf(`
$thumbprints = @(%s)
Get-ChildItem -LiteralPath Cert:\CurrentUser\Root -ErrorAction Stop |
	Where-Object { $_.Thumbprint -in $thumbprints } |
	Remove-Item -ErrorAction Stop
`, strings.Join(quotedFingerprints, ", "))
	_, err := s.powershell(ctx, script)
	return err
}

func certificatesFromJSON(out []byte) ([]Certificate, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	type rawCertificate struct {
		DER string
	}
	var raw []rawCertificate
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("parse Windows trusted certificates: %w", err)
	}

	certificates := make([]Certificate, 0, len(raw))
	for _, item := range raw {
		der, err := base64.StdEncoding.DecodeString(item.DER)
		if err != nil {
			continue
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			continue
		}
		certificates = append(certificates, Certificate{Fingerprint: certificateFingerprint(cert), X509: cert})
	}
	return certificates, nil
}

func (s *store) powershell(ctx context.Context, script string) ([]byte, error) {
	out, err := s.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return out, fmt.Errorf("powershell failed: %s: %w", bytes.TrimSpace(out), err)
	}
	return out, nil
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

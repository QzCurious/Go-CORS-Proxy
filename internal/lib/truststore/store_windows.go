//go:build windows

package truststore

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Store accesses the current user's Windows root certificate store.
type Store struct {
	runner commandRunner
}

// New returns the current user's operating-system trust store.
func New() *Store {
	return &Store{runner: execRunner{}}
}

// List returns a fresh, unordered snapshot containing one record for every
// parseable certificate in the current user's trust store.
func (s *Store) List(ctx context.Context) ([]Certificate, error) {
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

// Add adds a certificate file to the current user's trusted root store.
func (s *Store) Add(ctx context.Context, certificatePath string) error {
	script := fmt.Sprintf(`
Import-Certificate -FilePath %s -CertStoreLocation Cert:\CurrentUser\Root | Out-Null
`, psQuote(certificatePath))
	_, err := s.powershell(ctx, script)
	return err
}

// Remove removes every trust-store entry matching any supplied fingerprint.
// Missing fingerprints are successful.
func (s *Store) Remove(ctx context.Context, fingerprints []string) error {
	targets := removalTargets(fingerprints)
	if len(targets) == 0 {
		return nil
	}

	var errs []error
	for fingerprint := range targets {
		script := fmt.Sprintf(`
Remove-Item -Path %s -ErrorAction SilentlyContinue
`, psQuote(`Cert:\CurrentUser\Root\`+fingerprint))
		if _, err := s.powershell(ctx, script); err != nil {
			errs = append(errs, err)
		}
	}

	remaining, err := s.List(ctx)
	if err != nil {
		errs = append(errs, err)
	} else if err := remainingRemovalError(remaining, targets); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
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
	if bytes.HasPrefix(trimmed, []byte("{")) {
		var single rawCertificate
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return nil, fmt.Errorf("parse Windows trusted certificate: %w", err)
		}
		raw = []rawCertificate{single}
	} else if err := json.Unmarshal(trimmed, &raw); err != nil {
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

func (s *Store) powershell(ctx context.Context, script string) ([]byte, error) {
	out, err := s.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return out, fmt.Errorf("powershell failed: %s: %w", bytes.TrimSpace(out), err)
	}
	return out, nil
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

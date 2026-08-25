package truststore

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
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

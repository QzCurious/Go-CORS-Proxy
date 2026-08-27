package truststore

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
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

// Store exposes the current user's operating-system trust store as
// platform-neutral facts and mutations.
type Store interface {
	List(context.Context) ([]Certificate, error)
	Add(context.Context, string) error
	Remove(context.Context, []string) error
}

var _ func() (Store, error) = New

func certificateFingerprint(cert *x509.Certificate) string {
	sum := sha1.Sum(cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

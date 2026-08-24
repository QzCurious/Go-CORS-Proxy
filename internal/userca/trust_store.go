package userca

import (
	"context"
	"time"
)

type trustedCertificate struct {
	Fingerprint    string
	CertificatePEM []byte
	ExpiresAt      time.Time
}

type trustStore interface {
	// Returns trusted certificates matching the seamless-cors ownership footprint.
	trustedCertificates(ctx context.Context) ([]trustedCertificate, error)
	// Adds a certificate file as a trusted root for the current user.
	trust(ctx context.Context, certificatePath string) error
	// Deletes trusted certificates by fingerprint from the current user's store.
	remove(ctx context.Context, fingerprints []string) error
}

func newTrustStore() trustStore {
	return newPlatformTrustStore()
}

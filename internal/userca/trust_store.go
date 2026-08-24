package userca

import (
	"context"
	"crypto/x509"
	"time"
)

const ownedCACommonName = "seamless-cors Local CA"

type trustedCertificate struct {
	Fingerprint    string
	CertificatePEM []byte
	ExpiresAt      time.Time
}

type trustStore interface {
	// Returns trusted certificates matching the seamless-cors ownership footprint.
	TrustedCertificates(ctx context.Context) ([]trustedCertificate, error)
	// Adds a PEM certificate as a trusted root for the current user.
	Trust(ctx context.Context, certificatePEM []byte) error
	// Deletes trusted certificates by fingerprint from the current user's store.
	Remove(ctx context.Context, fingerprints []string) error
}

func isStrictFootprint(cert *x509.Certificate) bool {
	if cert.Subject.CommonName != ownedCACommonName {
		return false
	}
	if !cert.IsCA || !cert.BasicConstraintsValid {
		return false
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 || cert.KeyUsage&x509.KeyUsageCRLSign == 0 {
		return false
	}
	return cert.CheckSignatureFrom(cert) == nil
}

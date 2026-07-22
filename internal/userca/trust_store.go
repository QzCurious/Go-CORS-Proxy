package userca

import (
	"context"
	"crypto/x509"
	"errors"
	"time"
)

var ErrApprovalDenied = errors.New("certificate trust approval denied")

type TrustedCertificate struct {
	Fingerprint    string
	CertificatePEM []byte
	ExpiresAt      time.Time
}

type TrustStore interface {
	TrustedCertificates(ctx context.Context) ([]TrustedCertificate, error)
	Trust(ctx context.Context, certificatePEM []byte) error
	Remove(ctx context.Context, fingerprints []string) error
}

func NewTrustStore() TrustStore {
	return newTrustStore()
}

func isStrictFootprint(cert *x509.Certificate) bool {
	if cert.Subject.CommonName != CommonName {
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

package userca

import (
	"bytes"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"
)

func validateSigningMaterial(certificate tls.Certificate, now time.Time) (*tls.Certificate, error) {
	if len(certificate.Certificate) == 0 || certificate.PrivateKey == nil {
		return nil, fmt.Errorf("UserCA signing material is incomplete")
	}
	parsed, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("UserCA certificate is invalid: %w", err)
	}
	if certificate.Leaf != nil && !bytes.Equal(certificate.Leaf.Raw, parsed.Raw) {
		return nil, fmt.Errorf("UserCA parsed certificate does not match its chain")
	}
	certificate.Leaf = parsed
	signer, ok := certificate.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("UserCA private key cannot sign")
	}
	keyDER, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return nil, fmt.Errorf("UserCA private key is invalid: %w", err)
	}
	certificateDER, err := x509.MarshalPKIXPublicKey(parsed.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("UserCA public key is invalid: %w", err)
	}
	if !bytes.Equal(keyDER, certificateDER) {
		return nil, fmt.Errorf("UserCA private key does not match its certificate")
	}
	if !now.Before(parsed.NotAfter) {
		return nil, fmt.Errorf("UserCA certificate expired at %s", parsed.NotAfter.Format(time.RFC3339))
	}
	if now.Before(parsed.NotBefore) {
		return nil, fmt.Errorf("UserCA certificate is not valid until %s", parsed.NotBefore.Format(time.RFC3339))
	}
	if !parsed.IsCA || !parsed.BasicConstraintsValid || parsed.KeyUsage&x509.KeyUsageCertSign == 0 || parsed.KeyUsage&x509.KeyUsageCRLSign == 0 {
		return nil, fmt.Errorf("UserCA authority constraints are invalid")
	}
	if err := parsed.CheckSignatureFrom(parsed); err != nil {
		return nil, fmt.Errorf("UserCA certificate is not self-signed: %w", err)
	}
	return &certificate, nil
}

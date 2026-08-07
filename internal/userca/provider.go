package userca

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	leafValidity    = 30 * 24 * time.Hour
	leafCacheMaxAge = 24 * time.Hour
)

// ProvisioningDisposition describes the operational consequence of a failed
// leaf-certificate request. The string method is intentionally the only
// classification seam consumed by CORS Proxy; UserCA remains independent of
// that feature package.
type ProvisioningDisposition string

const (
	ProvisioningExpired        ProvisioningDisposition = "expired"
	ProvisioningInvalidRequest ProvisioningDisposition = "invalid-request"
	ProvisioningFailure        ProvisioningDisposition = "provider-failure"
)

var ErrProviderExpired = errors.New("HTTPS certificate provider expired")

// CertificateProvider is the UserCA-created capability used by Gateway and
// CORS Proxy. UserCA owns its signing policy, expiry checks, leaf validity,
// and cache; consumers only request a certificate for a host and inspect the
// deadline when coordinating lifecycle.
type CertificateProvider interface {
	CertificateFor(string) (*tls.Certificate, error)
	ValidUntil() time.Time
}

// ProviderError preserves the low-level cause while exposing only the
// operational disposition needed by the runtime seam.
type ProviderError struct {
	disposition ProvisioningDisposition
	err         error
}

func (e *ProviderError) Error() string { return e.err.Error() }

func (e *ProviderError) Unwrap() error { return e.err }

func (e *ProviderError) Disposition() string { return string(e.disposition) }

func newProviderError(disposition ProvisioningDisposition, err error) error {
	if err == nil {
		err = errors.New(string(disposition))
	}
	return &ProviderError{disposition: disposition, err: err}
}

type certificateProvider struct {
	certificate tls.Certificate
	validUntil  time.Time
	now         func() time.Time
	cache       *leafCache
}

func newCertificateProvider(certificate tls.Certificate, now func() time.Time) (CertificateProvider, error) {
	if now == nil {
		now = time.Now
	}
	if len(certificate.Certificate) == 0 || certificate.PrivateKey == nil {
		return nil, fmt.Errorf("HTTPS certificate provider requires complete signing material")
	}
	parsedLeaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("HTTPS certificate provider CA certificate is invalid: %w", err)
	}
	leaf := parsedLeaf
	if certificate.Leaf != nil && !bytes.Equal(certificate.Leaf.Raw, parsedLeaf.Raw) {
		return nil, fmt.Errorf("HTTPS certificate provider CA leaf does not match certificate chain")
	}
	certificate.Leaf = leaf
	signer, ok := certificate.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("HTTPS certificate provider key cannot sign certificates")
	}
	keyDER, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return nil, fmt.Errorf("HTTPS certificate provider signer key is invalid: %w", err)
	}
	certificateDER, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("HTTPS certificate provider CA public key is invalid: %w", err)
	}
	if !bytes.Equal(keyDER, certificateDER) {
		return nil, fmt.Errorf("HTTPS certificate provider signer does not match CA certificate")
	}
	nowValue := now()
	if !nowValue.Before(leaf.NotAfter) {
		return nil, fmt.Errorf("%w at %s", ErrProviderExpired, leaf.NotAfter.Format(time.RFC3339))
	}
	if nowValue.Before(leaf.NotBefore) {
		return nil, fmt.Errorf("HTTPS certificate provider CA is not valid until %s", leaf.NotBefore.Format(time.RFC3339))
	}
	if !leaf.IsCA || !leaf.BasicConstraintsValid || leaf.KeyUsage&x509.KeyUsageCertSign == 0 || leaf.KeyUsage&x509.KeyUsageCRLSign == 0 {
		return nil, fmt.Errorf("HTTPS certificate provider CA authority constraints are invalid")
	}
	if err := leaf.CheckSignatureFrom(leaf); err != nil {
		return nil, fmt.Errorf("HTTPS certificate provider CA is not self-signed: %w", err)
	}
	provider := &certificateProvider{
		certificate: certificate,
		validUntil:  leaf.NotAfter,
		now:         now,
		cache:       newLeafCache(),
	}
	if _, err := provider.issue("provider-self-test.example"); err != nil {
		return nil, fmt.Errorf("HTTPS certificate provider self-test failed: %w", err)
	}
	return provider, nil
}

func (p *certificateProvider) ValidUntil() time.Time { return p.validUntil }

func (p *certificateProvider) CertificateFor(hostname string) (*tls.Certificate, error) {
	if !p.now().Before(p.validUntil) {
		return nil, newProviderError(
			ProvisioningExpired,
			fmt.Errorf("%w at %s", ErrProviderExpired, p.validUntil.Format(time.RFC3339)),
		)
	}
	if err := validateHostname(hostname); err != nil {
		return nil, newProviderError(ProvisioningInvalidRequest, err)
	}
	certificate, err := p.cache.fetch(hostname, p.now(), func() (*tls.Certificate, error) {
		return p.issue(hostname)
	})
	if err != nil {
		var classified interface{ Disposition() string }
		if errors.As(err, &classified) {
			return nil, err
		}
		return nil, newProviderError(ProvisioningFailure, err)
	}
	if !p.now().Before(p.validUntil) {
		return nil, newProviderError(
			ProvisioningExpired,
			fmt.Errorf("%w at %s", ErrProviderExpired, p.validUntil.Format(time.RFC3339)),
		)
	}
	return certificate, nil
}

func (p *certificateProvider) issue(hostname string) (*tls.Certificate, error) {
	if !p.now().Before(p.validUntil) {
		return nil, newProviderError(
			ProvisioningExpired,
			fmt.Errorf("%w at %s", ErrProviderExpired, p.validUntil.Format(time.RFC3339)),
		)
	}
	caLeaf := p.certificate.Leaf
	if caLeaf == nil {
		var err error
		caLeaf, err = x509.ParseCertificate(p.certificate.Certificate[0])
		if err != nil {
			return nil, err
		}
	}
	signer, ok := p.certificate.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("CA private key cannot sign certificates")
	}
	now := p.now()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	notBefore := now.Add(-time.Minute)
	template := x509.Certificate{
		SerialNumber: serial,
		Issuer:       caLeaf.Subject,
		Subject: pkix.Name{
			CommonName:   hostname,
			Organization: []string{"seamless-cors local MITM proxy"},
		},
		NotBefore:             notBefore,
		NotAfter:              minTime(notBefore.Add(leafValidity), caLeaf.NotAfter),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if !notBefore.Before(template.NotAfter) {
		return nil, newProviderError(ProvisioningExpired, fmt.Errorf("%w before a valid leaf could be issued", ErrProviderExpired))
	}
	if ip := net.ParseIP(hostname); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{hostname}
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, caLeaf, &leafKey.PublicKey, signer)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	chain := make([][]byte, 1+len(p.certificate.Certificate))
	chain[0] = der
	copy(chain[1:], p.certificate.Certificate)
	return &tls.Certificate{
		Certificate: chain,
		PrivateKey:  leafKey,
		Leaf:        leaf,
	}, nil
}

func validateHostname(hostname string) error {
	if strings.TrimSpace(hostname) == "" || strings.ContainsAny(hostname, " /\\") {
		return fmt.Errorf("unsupported HTTPS hostname %q", hostname)
	}
	if net.ParseIP(hostname) != nil {
		return nil
	}
	if strings.ContainsAny(hostname, "[]:") || len(hostname) > 253 {
		return fmt.Errorf("unsupported HTTPS hostname %q", hostname)
	}
	trimmed := strings.TrimSuffix(hostname, ".")
	if trimmed == "" {
		return fmt.Errorf("unsupported HTTPS hostname %q", hostname)
	}
	for _, label := range strings.Split(trimmed, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("unsupported HTTPS hostname %q", hostname)
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return fmt.Errorf("unsupported HTTPS hostname %q", hostname)
			}
		}
	}
	return nil
}

type leafCache struct {
	mu    sync.Mutex
	certs map[string]cachedLeaf
}

type cachedLeaf struct {
	certificate *tls.Certificate
	createdAt   time.Time
}

func newLeafCache() *leafCache { return &leafCache{certs: map[string]cachedLeaf{}} }

func (c *leafCache) fetch(hostname string, now time.Time, generate func() (*tls.Certificate, error)) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.certs[hostname]; ok && now.Sub(cached.createdAt) <= leafCacheMaxAge {
		return cached.certificate, nil
	}
	certificate, err := generate()
	if err != nil {
		return nil, err
	}
	c.certs[hostname] = cachedLeaf{certificate: certificate, createdAt: now}
	return certificate, nil
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

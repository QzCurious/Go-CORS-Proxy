package userca

import (
	"bytes"
	"context"
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
	"time"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

type ProvisioningDisposition string

const (
	ProvisioningExpired        ProvisioningDisposition = "expired"
	ProvisioningInvalidRequest ProvisioningDisposition = "invalid-request"
	ProvisioningNotCovered     ProvisioningDisposition = "not-covered"
	ProvisioningFailure        ProvisioningDisposition = "provider-failure"
)

var ErrProviderExpired = errors.New("HTTPS certificate provider expired")

type ProviderSource interface {
	Project(context.Context, upstreamlist.Projection) (CertificateProvider, error)
	ValidUntil() time.Time
}

type CertificateProvider interface {
	CertificateFor(string) (*tls.Certificate, error)
	ValidUntil() time.Time
}

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

type providerSource struct {
	certificate tls.Certificate
	validUntil  time.Time
	now         func() time.Time
}

type certificateProvider struct {
	exact      map[string]*tls.Certificate
	wildcards  map[string]*tls.Certificate
	validUntil time.Time
	now        func() time.Time
}

func newProviderSource(certificate tls.Certificate, now func() time.Time) (ProviderSource, error) {
	if now == nil {
		now = time.Now
	}
	certificate, err := validateAuthority(certificate, now())
	if err != nil {
		return nil, err
	}
	source := &providerSource{
		certificate: certificate,
		validUntil:  certificate.Leaf.NotAfter,
		now:         now,
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("HTTPS provider source self-test key generation failed: %w", err)
	}
	if _, err := source.issue("provider-self-test.example", leafKey); err != nil {
		return nil, fmt.Errorf("HTTPS provider source self-test failed: %w", err)
	}
	return source, nil
}

func validateAuthority(certificate tls.Certificate, now time.Time) (tls.Certificate, error) {
	if len(certificate.Certificate) == 0 || certificate.PrivateKey == nil {
		return tls.Certificate{}, fmt.Errorf("HTTPS provider source requires complete signing material")
	}
	parsedLeaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("HTTPS provider source CA certificate is invalid: %w", err)
	}
	if certificate.Leaf != nil && !bytes.Equal(certificate.Leaf.Raw, parsedLeaf.Raw) {
		return tls.Certificate{}, fmt.Errorf("HTTPS provider source CA leaf does not match certificate chain")
	}
	certificate.Leaf = parsedLeaf
	signer, ok := certificate.PrivateKey.(crypto.Signer)
	if !ok {
		return tls.Certificate{}, fmt.Errorf("HTTPS provider source key cannot sign certificates")
	}
	keyDER, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("HTTPS provider source signer key is invalid: %w", err)
	}
	certificateDER, err := x509.MarshalPKIXPublicKey(parsedLeaf.PublicKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("HTTPS provider source CA public key is invalid: %w", err)
	}
	if !bytes.Equal(keyDER, certificateDER) {
		return tls.Certificate{}, fmt.Errorf("HTTPS provider source signer does not match CA certificate")
	}
	if !now.Before(parsedLeaf.NotAfter) {
		return tls.Certificate{}, fmt.Errorf("%w at %s", ErrProviderExpired, parsedLeaf.NotAfter.Format(time.RFC3339))
	}
	if now.Before(parsedLeaf.NotBefore) {
		return tls.Certificate{}, fmt.Errorf("HTTPS provider source CA is not valid until %s", parsedLeaf.NotBefore.Format(time.RFC3339))
	}
	if !parsedLeaf.IsCA || !parsedLeaf.BasicConstraintsValid || parsedLeaf.KeyUsage&x509.KeyUsageCertSign == 0 || parsedLeaf.KeyUsage&x509.KeyUsageCRLSign == 0 {
		return tls.Certificate{}, fmt.Errorf("HTTPS provider source CA authority constraints are invalid")
	}
	if err := parsedLeaf.CheckSignatureFrom(parsedLeaf); err != nil {
		return tls.Certificate{}, fmt.Errorf("HTTPS provider source CA is not self-signed: %w", err)
	}
	return certificate, nil
}

func (s *providerSource) ValidUntil() time.Time { return s.validUntil }

func (s *providerSource) Project(ctx context.Context, projection upstreamlist.Projection) (CertificateProvider, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.now().Before(s.validUntil) {
		return nil, fmt.Errorf("%w at %s", ErrProviderExpired, s.validUntil.Format(time.RFC3339))
	}
	exact, wildcards := certificateIdentities(projection)
	provider := &certificateProvider{
		exact:      make(map[string]*tls.Certificate, len(exact)),
		wildcards:  make(map[string]*tls.Certificate, len(wildcards)),
		validUntil: s.validUntil,
		now:        s.now,
	}
	if len(exact)+len(wildcards) == 0 {
		return provider, nil
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate provider leaf key: %w", err)
	}
	for _, hostname := range exact {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		certificate, err := s.issue(hostname, leafKey)
		if err != nil {
			return nil, fmt.Errorf("generate Selector Certificate for %q: %w", hostname, err)
		}
		provider.exact[lookupHostname(hostname)] = certificate
	}
	for _, hostname := range wildcards {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		certificate, err := s.issue("*."+hostname, leafKey)
		if err != nil {
			return nil, fmt.Errorf("generate Selector Certificate for %q: %w", "*."+hostname, err)
		}
		provider.wildcards[lookupHostname(hostname)] = certificate
	}
	return provider, nil
}

func certificateIdentities(projection upstreamlist.Projection) ([]string, []string) {
	var exact []string
	var wildcards []string
	seenExact := make(map[string]struct{})
	seenWildcards := make(map[string]struct{})
	appendExact := func(hostname string) {
		key := lookupHostname(hostname)
		if _, ok := seenExact[key]; ok {
			return
		}
		seenExact[key] = struct{}{}
		exact = append(exact, hostname)
	}
	appendWildcard := func(hostname string) {
		key := lookupHostname(hostname)
		if _, ok := seenWildcards[key]; ok {
			return
		}
		seenWildcards[key] = struct{}{}
		wildcards = append(wildcards, hostname)
	}
	for _, selector := range projection.HostSelectors {
		if selector.Wildcard {
			appendWildcard(selector.Hostname)
			continue
		}
		appendExact(selector.Hostname)
	}
	for _, selector := range projection.OriginSelectors {
		if selector.Scheme == "https" {
			appendExact(selector.Hostname)
		}
	}
	return exact, wildcards
}

func (s *providerSource) issue(identity string, leafKey *rsa.PrivateKey) (*tls.Certificate, error) {
	if !s.now().Before(s.validUntil) {
		return nil, fmt.Errorf("%w at %s", ErrProviderExpired, s.validUntil.Format(time.RFC3339))
	}
	signer, ok := s.certificate.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("CA private key cannot sign certificates")
	}
	now := s.now()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Issuer:       s.certificate.Leaf.Subject,
		Subject: pkix.Name{
			CommonName:   identity,
			Organization: []string{"seamless-cors local MITM proxy"},
		},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              s.validUntil,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(identity); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{identity}
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, s.certificate.Leaf, &leafKey.PublicKey, signer)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	chain := make([][]byte, 1+len(s.certificate.Certificate))
	chain[0] = der
	copy(chain[1:], s.certificate.Certificate)
	return &tls.Certificate{Certificate: chain, PrivateKey: leafKey, Leaf: leaf}, nil
}

func (p *certificateProvider) ValidUntil() time.Time { return p.validUntil }

func (p *certificateProvider) CertificateFor(hostname string) (*tls.Certificate, error) {
	if !p.now().Before(p.validUntil) {
		return nil, newProviderError(
			ProvisioningExpired,
			fmt.Errorf("%w at %s", ErrProviderExpired, p.validUntil.Format(time.RFC3339)),
		)
	}
	if err := validateRequestHostname(hostname); err != nil {
		return nil, newProviderError(ProvisioningInvalidRequest, err)
	}
	key := lookupHostname(hostname)
	if certificate := p.exact[key]; certificate != nil {
		return certificate, nil
	}
	if net.ParseIP(hostname) == nil {
		if dot := strings.IndexByte(key, '.'); dot > 0 {
			if certificate := p.wildcards[key[dot+1:]]; certificate != nil {
				return certificate, nil
			}
		}
	}
	return nil, newProviderError(
		ProvisioningNotCovered,
		fmt.Errorf("HTTPS hostname %q is outside the configured certificate scope", hostname),
	)
}

func validateRequestHostname(hostname string) error {
	if hostname == "" || strings.TrimSpace(hostname) != hostname || strings.ContainsAny(hostname, " /\\") {
		return fmt.Errorf("unsupported HTTPS hostname %q", hostname)
	}
	if net.ParseIP(strings.Trim(hostname, "[]")) != nil {
		return nil
	}
	if strings.ContainsAny(hostname, "[]:*") || strings.HasSuffix(hostname, ".") || len(hostname) > 253 {
		return fmt.Errorf("unsupported HTTPS hostname %q", hostname)
	}
	for _, label := range strings.Split(hostname, ".") {
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

func lookupHostname(hostname string) string {
	trimmed := strings.Trim(hostname, "[]")
	if ip := net.ParseIP(trimmed); ip != nil {
		return ip.String()
	}
	return strings.ToLower(trimmed)
}

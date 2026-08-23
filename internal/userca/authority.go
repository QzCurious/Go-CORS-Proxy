package userca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// authority is one immutable fingerprint-named local certificate generation.
type authority struct {
	certPath string
	keyPath  string
	certPEM  []byte
	keyPEM   []byte
	cert     *x509.Certificate
}

func createCandidate(dir string, now func() time.Time) (*authority, error) {
	// Phase 1: generate a self-signed development authority entirely in memory.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	createdAt := now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(createdAt.UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             createdAt.Add(-time.Minute),
		NotAfter:              createdAt.Add(validity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	fingerprint, err := sha1Fingerprint(certPEM)
	if err != nil {
		return nil, err
	}

	// Phase 2: durably publish the immutable generation before trust mutation.
	authoritiesDir := filepath.Join(dir, authoritiesDirName)
	if err := os.MkdirAll(authoritiesDir, 0o700); err != nil {
		return nil, err
	}
	generationDir := filepath.Join(authoritiesDir, fingerprint)
	if err := os.Mkdir(generationDir, 0o700); err != nil {
		return nil, err
	}
	certPath := filepath.Join(generationDir, certFileName)
	keyPath := filepath.Join(generationDir, keyFileName)
	if err := writeDurableFile(certPath, certPEM, 0o600); err != nil {
		_ = os.RemoveAll(generationDir)
		return nil, err
	}
	if err := writeDurableFile(keyPath, keyPEM, 0o600); err != nil {
		_ = os.RemoveAll(generationDir)
		return nil, err
	}
	if err := errors.Join(
		syncDirectory(generationDir),
		syncDirectory(authoritiesDir),
		syncDirectory(dir),
		syncDirectory(filepath.Dir(dir)),
	); err != nil {
		_ = os.RemoveAll(generationDir)
		return nil, err
	}
	return &authority{certPath: certPath, keyPath: keyPath, certPEM: certPEM, keyPEM: keyPEM, cert: template}, nil
}

func loadGeneration(dir, fingerprint string) (*authority, error) {
	if !validFingerprint(fingerprint) {
		return nil, fmt.Errorf("active UserCA fingerprint is invalid")
	}
	generationDir := filepath.Join(dir, authoritiesDirName, fingerprint)
	certPath := filepath.Join(generationDir, certFileName)
	keyPath := filepath.Join(generationDir, keyFileName)
	certPEM, err := readFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := readFile(keyPath)
	if err != nil {
		return nil, err
	}
	active, err := parseAuthority(certPath, keyPath, certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidAuthority, err)
	}
	return active, nil
}

func parseAuthority(certPath, keyPath string, certPEM, keyPEM []byte) (*authority, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("CA certificate PEM is invalid")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		return nil, fmt.Errorf("CA key PEM is invalid")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}
	if cert.Subject.CommonName != commonName || !cert.IsCA || !cert.BasicConstraintsValid {
		return nil, fmt.Errorf("CA certificate identity is invalid")
	}
	certKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("CA certificate public key is not RSA")
	}
	if certKey.N.Cmp(key.PublicKey.N) != 0 || certKey.E != key.PublicKey.E {
		return nil, fmt.Errorf("CA certificate and key do not match")
	}
	return &authority{certPath: certPath, keyPath: keyPath, certPEM: certPEM, keyPEM: keyPEM, cert: cert}, nil
}

func (a *authority) fingerprint() (string, error) {
	return sha1Fingerprint(a.certPEM)
}

func (a *authority) tlsCertificate() (tls.Certificate, error) {
	certificate, err := tls.X509KeyPair(a.certPEM, a.keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificate.Leaf, err = x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return tls.Certificate{}, err
	}
	return certificate, nil
}

func sha1Fingerprint(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("CA certificate PEM is invalid")
	}
	sum := sha1.Sum(block.Bytes)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

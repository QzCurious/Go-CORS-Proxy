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

const (
	certFileName = "certificate.pem"
	keyFileName  = "private-key.pem"
	validity     = 5 * 365 * 24 * time.Hour
)

var errInvalidAuthority = errors.New("UserCA authority material is invalid")

// authority is the one locally persisted certificate and private-key pair.
type authority struct {
	certPath string
	keyPath  string
	certPEM  []byte
	keyPEM   []byte
	cert     *x509.Certificate
}

func createAuthority(dir string, now func() time.Time) (*authority, error) {
	// Generate a self-signed development authority entirely in memory.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	createdAt := now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(createdAt.UnixNano()),
		Subject:               pkix.Name{CommonName: ownedCACommonName},
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

	// Persist directly into the final directory. Callers establish an absent
	// precondition, and a later explicit install reconciles interrupted writes.
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, err
	}
	certPath := filepath.Join(dir, certFileName)
	keyPath := filepath.Join(dir, keyFileName)
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	return &authority{certPath: certPath, keyPath: keyPath, certPEM: certPEM, keyPEM: keyPEM, cert: template}, nil
}

var readFile = os.ReadFile

func loadAuthority(dir string) (*authority, error) {
	certPath := filepath.Join(dir, certFileName)
	keyPath := filepath.Join(dir, keyFileName)
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
	if cert.Subject.CommonName != ownedCACommonName || !cert.IsCA || !cert.BasicConstraintsValid {
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

package userca

import (
	"crypto"
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

// authority is the one locally persisted certificate and private-key pair.
type authority struct {
	certPath string
	keyPath  string
	cert     *x509.Certificate
	key      crypto.PrivateKey
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
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

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
	return &authority{certPath: certPath, keyPath: keyPath, cert: cert, key: key}, nil
}

func loadAuthority(dir string) (*authority, error) {
	certPath := filepath.Join(dir, certFileName)
	keyPath := filepath.Join(dir, keyFileName)

	// Read the persisted pair before interpreting either file so filesystem
	// failures remain distinct from invalid authority material.
	certPEM, err := readFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := readFile(keyPath)
	if err != nil {
		return nil, err
	}

	// Parse and validate the complete pair so no partial semantic authority
	// escapes this load phase.
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidAuthority, err)
	}
	return &authority{certPath: certPath, keyPath: keyPath, cert: pair.Leaf, key: pair.PrivateKey}, nil
}

func (a *authority) fingerprint() string {
	sum := sha1.Sum(a.cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func (a *authority) tlsCertificate() tls.Certificate {
	return tls.Certificate{
		Certificate: [][]byte{a.cert.Raw},
		PrivateKey:  a.key,
		Leaf:        a.cert,
	}
}

func isOwnedAuthorityCertificate(cert *x509.Certificate) bool {
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

func isAuthorityUsable(cert *x509.Certificate, now time.Time) bool {
	return isOwnedAuthorityCertificate(cert) &&
		!now.Before(cert.NotBefore) &&
		now.Before(cert.NotAfter)
}

const (
	ownedCACommonName = "seamless-cors Local CA"
	certFileName      = "certificate.pem"
	keyFileName       = "private-key.pem"
	validity          = 5 * 365 * 24 * time.Hour
)

var errInvalidAuthority = errors.New("UserCA authority material is invalid")

var readFile = os.ReadFile

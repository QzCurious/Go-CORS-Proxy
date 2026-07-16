package userca

import (
	"context"
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

	"seamless-cors/internal/platform"
)

const (
	CommonName      = "seamless-cors Installed User CA"
	CertFileName    = "root-ca.pem"
	KeyFileName     = "root-ca-key.pem"
	Validity        = 5 * 365 * 24 * time.Hour
	RenewalWindow   = 30 * 24 * time.Hour
	LeafValidity    = 30 * 24 * time.Hour
	LeafCacheMaxAge = 24 * time.Hour
)

type TrustStore interface {
	TrustedCAs() ([]platform.CARecord, error)
	TrustCA(ctx context.Context, certPEM []byte) error
	RemoveCAs(ctx context.Context, fingerprints []string) error
}

type Health string

const (
	HealthUsable             Health = "usable"
	HealthMissing            Health = "missing"
	HealthExpired            Health = "expired"
	HealthExpiringSoon       Health = "expiring-soon"
	HealthInvalid            Health = "invalid"
	HealthMultiple           Health = "multiple"
	HealthMismatchedMaterial Health = "mismatched-material"
	HealthUnsupported        Health = "unsupported"
	HealthUnknown            Health = "unknown"
)

type Report struct {
	Health  Health
	Expires time.Time
}

type EnsureResult struct {
	Report
	Changed     bool
	Fingerprint string
}

type Authority struct {
	CertPath string
	KeyPath  string
	CertPEM  []byte
	KeyPEM   []byte
	cert     *x509.Certificate
	key      *rsa.PrivateKey
}

func Inspect(dir string, store TrustStore) (Report, error) {
	report, _, err := inspect(dir, store, false)
	return report, err
}

func Ensure(dir string, store TrustStore) (*Authority, EnsureResult, error) {
	return EnsureContext(context.Background(), dir, store)
}

func EnsureContext(ctx context.Context, dir string, store TrustStore) (*Authority, EnsureResult, error) {
	lease, err := acquireCAMutationLease(ctx, dir)
	if err != nil {
		return nil, EnsureResult{}, err
	}
	defer func() { _ = lease.release() }()
	return ensureLocked(ctx, dir, store)
}

func ensureLocked(ctx context.Context, dir string, store TrustStore) (*Authority, EnsureResult, error) {
	report, authority, err := inspect(dir, store, true)
	if err != nil {
		return nil, EnsureResult{Report: report}, err
	}
	if err := ctx.Err(); err != nil {
		return reconcileCancelledEnsure(dir, store, err)
	}
	if report.Health == HealthUsable {
		return authority, ensureResult(authority, report, false), nil
	}
	if err := uninstallLocked(ctx, dir, store); err != nil {
		if ctx.Err() != nil {
			return reconcileCancelledEnsure(dir, store, ctx.Err())
		}
		return nil, EnsureResult{Report: report}, err
	}
	if err := ctx.Err(); err != nil {
		return reconcileCancelledEnsure(dir, store, err)
	}
	authority, err = createFresh(ctx, dir, store)
	if err != nil {
		if ctx.Err() != nil {
			return reconcileCancelledEnsure(dir, store, ctx.Err())
		}
		cleanupErr := uninstallLocked(context.Background(), dir, store)
		return nil, EnsureResult{Report: Report{Health: HealthMissing}}, errors.Join(err, cleanupErr)
	}
	if ctx.Err() != nil {
		return reconcileCancelledEnsure(dir, store, ctx.Err())
	}
	return authority, ensureResult(authority, Report{
		Health:  HealthUsable,
		Expires: authority.cert.NotAfter,
	}, true), nil
}

func reconcileCancelledEnsure(dir string, store TrustStore, cancellation error) (*Authority, EnsureResult, error) {
	report, authority, err := inspect(dir, store, true)
	if err == nil && report.Health == HealthUsable {
		return authority, ensureResult(authority, report, true), nil
	}
	cleanupErr := uninstallLocked(context.Background(), dir, store)
	return nil, EnsureResult{Report: Report{Health: HealthMissing}}, errors.Join(cancellation, err, cleanupErr)
}

func ensureResult(authority *Authority, report Report, changed bool) EnsureResult {
	fingerprint, _ := authority.Fingerprint()
	return EnsureResult{Report: report, Changed: changed, Fingerprint: fingerprint}
}

func Uninstall(dir string, store TrustStore) error {
	return UninstallContext(context.Background(), dir, store)
}

func UninstallContext(ctx context.Context, dir string, store TrustStore) error {
	lease, err := acquireCAMutationLease(ctx, dir)
	if err != nil {
		return err
	}
	defer func() { _ = lease.release() }()
	err = uninstallLocked(ctx, dir, store)
	if ctx.Err() == nil {
		return err
	}
	// Once removal has begun, settle partial trust-store changes to the
	// operation's safe terminal state even though its caller was cancelled.
	reconcileErr := uninstallLocked(context.Background(), dir, store)
	return errors.Join(ctx.Err(), err, reconcileErr)
}

func uninstallLocked(ctx context.Context, dir string, store TrustStore) error {
	records, trustErr := store.TrustedCAs()
	var fingerprints []string
	for _, record := range records {
		fingerprints = append(fingerprints, record.SHA1)
	}
	removeErr := store.RemoveCAs(ctx, fingerprints)
	fileErr := errors.Join(os.RemoveAll(dir), os.RemoveAll(stagingDir(dir)))
	return errors.Join(trustErr, removeErr, fileErr)
}

func Load(dir string) (*Authority, error) {
	certPath := filepath.Join(dir, CertFileName)
	keyPath := filepath.Join(dir, KeyFileName)
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	return parseAuthority(certPath, keyPath, certPEM, keyPEM)
}

// LoadUsable loads the currently trusted and locally usable authority without
// installing or replacing trust. A nil authority and a non-usable report are
// returned when the Installed User CA is not ready for admission.
func LoadUsable(dir string, store TrustStore) (*Authority, Report, error) {
	return LoadUsableContext(context.Background(), dir, store)
}

// LoadUsableContext waits for any CA mutation to settle before reinspecting the
// current authority. It never invokes platform trust approval.
func LoadUsableContext(ctx context.Context, dir string, store TrustStore) (*Authority, Report, error) {
	lease, err := acquireCAMutationLease(ctx, dir)
	if err != nil {
		return nil, Report{Health: HealthUnknown}, err
	}
	defer func() { _ = lease.release() }()
	report, authority, err := inspect(dir, store, true)
	if err != nil {
		return nil, report, err
	}
	if report.Health != HealthUsable {
		return nil, report, nil
	}
	return authority, report, nil
}

// Fingerprint identifies the authority certificate independently of its local
// paths and key representation.
func (a *Authority) Fingerprint() (string, error) {
	return SHA1Fingerprint(a.CertPEM)
}

func (a *Authority) TLSCertificate() (tls.Certificate, error) {
	cert, err := tls.X509KeyPair(a.CertPEM, a.KeyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, err
	}
	return cert, nil
}

func inspect(dir string, store TrustStore, repairPermissions bool) (Report, *Authority, error) {
	records, err := store.TrustedCAs()
	if err != nil {
		return Report{Health: HealthUnknown}, nil, err
	}
	switch len(records) {
	case 0:
		return Report{Health: HealthMissing}, nil, nil
	case 1:
	default:
		return Report{Health: HealthMultiple}, nil, nil
	}
	authority, err := Load(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Report{Health: HealthMismatchedMaterial}, nil, nil
		}
		return Report{Health: HealthInvalid}, nil, nil
	}
	fingerprint, err := SHA1Fingerprint(authority.CertPEM)
	if err != nil || fingerprint != records[0].SHA1 {
		return Report{Health: HealthMismatchedMaterial}, nil, nil
	}
	now := time.Now()
	if !now.Before(authority.cert.NotAfter) {
		return Report{Health: HealthExpired, Expires: authority.cert.NotAfter}, nil, nil
	}
	if now.Add(RenewalWindow).After(authority.cert.NotAfter) {
		return Report{Health: HealthExpiringSoon, Expires: authority.cert.NotAfter}, nil, nil
	}
	if repairPermissions {
		if err := repairAuthorityPermissions(dir, authority); err != nil {
			return Report{Health: HealthInvalid, Expires: authority.cert.NotAfter}, nil, err
		}
	}
	return Report{Health: HealthUsable, Expires: authority.cert.NotAfter}, authority, nil
}

var chmod = os.Chmod

func repairAuthorityPermissions(dir string, authority *Authority) error {
	return errors.Join(
		chmod(dir, 0o700),
		chmod(authority.CertPath, 0o600),
		chmod(authority.KeyPath, 0o600),
	)
}

func createFresh(ctx context.Context, dir string, store TrustStore) (*Authority, error) {
	staging := stagingDir(dir)
	if err := os.RemoveAll(staging); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: CommonName},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(Validity),
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
	stagedCertPath := filepath.Join(staging, CertFileName)
	stagedKeyPath := filepath.Join(staging, KeyFileName)
	if err := os.WriteFile(stagedCertPath, certPEM, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(stagedKeyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := store.TrustCA(ctx, certPEM); err != nil {
		return nil, err
	}
	if err := os.Rename(staging, dir); err != nil {
		return nil, err
	}
	certPath := filepath.Join(dir, CertFileName)
	keyPath := filepath.Join(dir, KeyFileName)
	return &Authority{
		CertPath: certPath,
		KeyPath:  keyPath,
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		cert:     template,
		key:      key,
	}, nil
}

func stagingDir(dir string) string {
	return filepath.Clean(dir) + ".staging"
}

func parseAuthority(certPath, keyPath string, certPEM, keyPEM []byte) (*Authority, error) {
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
	if cert.Subject.CommonName != CommonName || !cert.IsCA || !cert.BasicConstraintsValid {
		return nil, fmt.Errorf("CA certificate identity is invalid")
	}
	certKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("CA certificate public key is not RSA")
	}
	if certKey.N.Cmp(key.PublicKey.N) != 0 || certKey.E != key.PublicKey.E {
		return nil, fmt.Errorf("CA certificate and key do not match")
	}
	return &Authority{
		CertPath: certPath,
		KeyPath:  keyPath,
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		cert:     cert,
		key:      key,
	}, nil
}

func SHA1Fingerprint(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("CA certificate PEM is invalid")
	}
	sum := sha1.Sum(block.Bytes)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

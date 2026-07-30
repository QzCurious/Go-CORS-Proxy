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
	"runtime"
	"strings"
	"time"
)

const (
	CommonName                = "seamless-cors Installed User CA"
	AuthoritiesDirName        = "authorities"
	ActiveFingerprintFileName = "active-fingerprint"
	CertFileName              = "certificate.pem"
	KeyFileName               = "private-key.pem"
	Validity                  = 5 * 365 * 24 * time.Hour
	RenewalWindow             = 30 * 24 * time.Hour
	LeafValidity              = 30 * 24 * time.Hour
	LeafCacheMaxAge           = 24 * time.Hour
)

type Health string

const (
	HealthUsable             Health = "usable"
	HealthMissing            Health = "missing"
	HealthExpired            Health = "expired"
	HealthExpiringSoon       Health = "expiring-soon"
	HealthInvalid            Health = "invalid"
	HealthMultiple           Health = "multiple"
	HealthMismatchedMaterial Health = "mismatched-material"
	HealthUnknown            Health = "unknown"
)

type Report struct {
	Health         Health
	Expires        time.Time
	NonActiveCount int
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
	return InspectContext(context.Background(), dir, store)
}

func InspectContext(ctx context.Context, dir string, store TrustStore) (Report, error) {
	lease, err := acquireCAMutationLease(ctx, dir)
	if err != nil {
		return Report{Health: HealthUnknown}, err
	}
	defer func() { _ = lease.release() }()
	report, _, err := inspectLocked(ctx, dir, store, false)
	return report, err
}

func Ensure(dir string, store TrustStore) (*Authority, EnsureResult, error) {
	return EnsureContext(context.Background(), dir, store)
}

// EnsureContext reuses a healthy Active authority and rotates every other
// state through a trusted immutable generation. Candidate trust is installed
// before the atomic Active marker is committed.
func EnsureContext(ctx context.Context, dir string, store TrustStore) (*Authority, EnsureResult, error) {
	return EnsureAndAdoptContext(ctx, dir, store, nil)
}

// EnsureAndAdoptContext keeps the CA mutation lease through the optional
// runtime adoption callback. For a rotation, the durable Active marker is
// committed before adoption and remains authoritative if adoption fails.
func EnsureAndAdoptContext(
	ctx context.Context,
	dir string,
	store TrustStore,
	adopt func(*Authority, Report) error,
) (*Authority, EnsureResult, error) {
	lease, err := acquireCAMutationLease(ctx, dir)
	if err != nil {
		return nil, EnsureResult{}, err
	}
	defer func() { _ = lease.release() }()

	report, active, inspectErr := inspectLocked(ctx, dir, store, true)
	if inspectErr != nil && report.Health == HealthUnknown {
		return nil, EnsureResult{Report: report}, inspectErr
	}
	if err := cleanupNonActiveLocked(ctx, dir, store); err != nil {
		// A valid Active generation remains usable even when residue cleanup
		// fails. Return it so a caller can still rebuild in-memory interception.
		if active != nil && (report.Health == HealthUsable || report.Health == HealthExpiringSoon) {
			adoptErr := adoptAuthority(adopt, active, report)
			return active, ensureResult(active, report, false), errors.Join(&NonActiveCleanupError{Cause: err}, adoptErr)
		}
		return nil, EnsureResult{Report: report}, &NonActiveCleanupError{Cause: err}
	}
	report.NonActiveCount = 0
	if active != nil && report.Health == HealthUsable {
		return active, ensureResult(active, report, false), adoptAuthority(adopt, active, report)
	}
	if active != nil && report.Health == HealthMismatchedMaterial {
		if err := store.Trust(ctx, active.CertPEM); err != nil {
			return nil, EnsureResult{Report: report}, err
		}
		repaired := Report{Health: HealthUsable, Expires: active.cert.NotAfter}
		if time.Now().Add(RenewalWindow).After(active.cert.NotAfter) {
			repaired.Health = HealthExpiringSoon
		}
		if repaired.Health == HealthUsable {
			return active, ensureResult(active, repaired, true), adoptAuthority(adopt, active, repaired)
		}
		report = repaired
	}
	if err := ctx.Err(); err != nil {
		return nil, EnsureResult{Report: report}, err
	}

	candidate, err := createCandidate(dir)
	if err != nil {
		return nil, EnsureResult{Report: report}, err
	}
	fingerprint, err := candidate.Fingerprint()
	if err != nil {
		_ = os.RemoveAll(filepath.Dir(candidate.CertPath))
		return nil, EnsureResult{Report: report}, err
	}
	if err := store.Trust(ctx, candidate.CertPEM); err != nil {
		cleanupErr := cleanupCandidate(context.Background(), dir, store, fingerprint)
		return nil, EnsureResult{Report: report}, candidateOperationError(err, cleanupErr)
	}

	// Trust may be interactive and cancellable. Once it has succeeded, commit
	// or compensate without observing caller cancellation.
	markerCommitted, markerErr := writeActiveFingerprint(dir, fingerprint)
	if markerErr != nil && !markerCommitted {
		cleanupErr := cleanupCandidate(context.Background(), dir, store, fingerprint)
		return nil, EnsureResult{Report: report}, candidateOperationError(markerErr, cleanupErr)
	}
	nonActive := 0
	if records, recordsErr := store.TrustedCertificates(context.Background()); recordsErr == nil {
		nonActive = nonActiveCount(records, fingerprint)
	}
	removeNonActivePrivateKeys(dir, fingerprint)
	committedReport := Report{
		Health:         HealthUsable,
		Expires:        candidate.cert.NotAfter,
		NonActiveCount: nonActive,
	}
	adoptErr := adoptAuthority(adopt, candidate, committedReport)
	return candidate, ensureResult(candidate, committedReport, true), errors.Join(markerErr, adoptErr)
}

type NonActiveCleanupError struct {
	Cause error
}

func (e *NonActiveCleanupError) Error() string {
	return fmt.Sprintf("non-active UserCA cleanup failed: %v", e.Cause)
}

func (e *NonActiveCleanupError) Unwrap() error { return e.Cause }

func candidateOperationError(operationErr, cleanupErr error) error {
	if cleanupErr == nil {
		return operationErr
	}
	return errors.Join(operationErr, &NonActiveCleanupError{Cause: cleanupErr})
}

func adoptAuthority(adopt func(*Authority, Report) error, authority *Authority, report Report) error {
	if adopt == nil {
		return nil
	}
	return adopt(authority, report)
}

func ensureResult(authority *Authority, report Report, changed bool) EnsureResult {
	fingerprint, _ := authority.Fingerprint()
	return EnsureResult{Report: report, Changed: changed, Fingerprint: fingerprint}
}

func CleanupNonActiveContext(ctx context.Context, dir string, store TrustStore) error {
	lease, err := acquireCAMutationLease(ctx, dir)
	if err != nil {
		return err
	}
	defer func() { _ = lease.release() }()
	return cleanupNonActiveLocked(ctx, dir, store)
}

func Uninstall(dir string, store TrustStore) error {
	return UninstallContext(context.Background(), dir, store)
}

func UninstallContext(ctx context.Context, dir string, store TrustStore) error {
	return UninstallWithCommitContext(ctx, dir, store, nil)
}

// UninstallWithCommitContext acquires the mutation lease before invoking the
// non-cancellable readiness-loss commit and removing every owned authority.
func UninstallWithCommitContext(ctx context.Context, dir string, store TrustStore, commit func()) error {
	lease, err := acquireCAMutationLease(ctx, dir)
	if err != nil {
		return err
	}
	defer func() { _ = lease.release() }()
	// Admission is cancellable; once admitted, complete removal even if the
	// command transport disconnects.
	if commit != nil {
		commit()
	}
	return uninstallLocked(context.Background(), dir, store)
}

func uninstallLocked(ctx context.Context, dir string, store TrustStore) error {
	records, trustErr := store.TrustedCertificates(ctx)
	fingerprints := fingerprints(records)
	var removeErr error
	if len(fingerprints) > 0 {
		removeErr = store.Remove(ctx, fingerprints)
	}
	fileErr := os.RemoveAll(dir)
	return errors.Join(trustErr, removeErr, fileErr)
}

func Load(dir string) (*Authority, error) {
	fingerprint, err := readActiveFingerprint(dir)
	if err != nil {
		return nil, err
	}
	return loadGeneration(dir, fingerprint)
}

// LoadHTTPSReadyContext assesses the marked Active authority without
// installing, repairing, or guessing from non-active authority state.
func LoadHTTPSReadyContext(ctx context.Context, dir string, store TrustStore) (*Authority, Report, error) {
	var authority *Authority
	var report Report
	var assessmentErr error
	err := UseHTTPSReadinessContext(ctx, dir, store, func(a *Authority, r Report, err error) error {
		authority, report, assessmentErr = a, r, err
		return nil
	})
	if err != nil {
		return nil, Report{Health: HealthUnknown}, err
	}
	return authority, report, assessmentErr
}

// UseHTTPSReadinessContext keeps the mutation lease while the caller admits
// the assessed generation into a runtime. Assessment failures are supplied to
// use so start can remain available in HTTP-only mode.
func UseHTTPSReadinessContext(
	ctx context.Context,
	dir string,
	store TrustStore,
	use func(*Authority, Report, error) error,
) error {
	lease, err := acquireCAMutationLease(ctx, dir)
	if err != nil {
		return err
	}
	defer func() { _ = lease.release() }()
	report, authority, err := inspectLocked(ctx, dir, store, false)
	if err != nil {
		return use(nil, report, err)
	}
	if report.Health != HealthUsable && report.Health != HealthExpiringSoon {
		authority = nil
	}
	return use(authority, report, nil)
}

func (a *Authority) Fingerprint() (string, error) {
	return SHA1Fingerprint(a.CertPEM)
}

func (a *Authority) ExpiresAt() time.Time {
	if a == nil || a.cert == nil {
		return time.Time{}
	}
	return a.cert.NotAfter
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

func inspectLocked(ctx context.Context, dir string, store TrustStore, repairPermissions bool) (Report, *Authority, error) {
	records, err := store.TrustedCertificates(ctx)
	if err != nil {
		return Report{Health: HealthUnknown}, nil, err
	}
	fingerprint, err := readActiveFingerprint(dir)
	if err != nil {
		if os.IsNotExist(err) {
			residue := nonActiveResidueCount(dir, "", records)
			if residue == 0 {
				return Report{Health: HealthMissing}, nil, nil
			}
			return Report{Health: HealthInvalid, NonActiveCount: residue}, nil, nil
		}
		return Report{Health: HealthInvalid, NonActiveCount: nonActiveResidueCount(dir, "", records)}, nil, err
	}
	authority, err := loadGeneration(dir, fingerprint)
	if err != nil {
		if os.IsNotExist(err) {
			return Report{Health: HealthMismatchedMaterial, NonActiveCount: nonActiveResidueCount(dir, fingerprint, records)}, nil, nil
		}
		return Report{Health: HealthInvalid, NonActiveCount: nonActiveResidueCount(dir, fingerprint, records)}, nil, nil
	}
	actualFingerprint, err := authority.Fingerprint()
	if err != nil || actualFingerprint != fingerprint {
		return Report{Health: HealthMismatchedMaterial, NonActiveCount: nonActiveResidueCount(dir, fingerprint, records)}, nil, nil
	}
	if !containsFingerprint(records, fingerprint) {
		return Report{Health: HealthMismatchedMaterial, Expires: authority.cert.NotAfter, NonActiveCount: nonActiveResidueCount(dir, fingerprint, records)}, authority, nil
	}
	now := time.Now()
	report := Report{Expires: authority.cert.NotAfter, NonActiveCount: nonActiveResidueCount(dir, fingerprint, records)}
	if !now.Before(authority.cert.NotAfter) {
		report.Health = HealthExpired
		return report, nil, nil
	}
	if now.Add(RenewalWindow).After(authority.cert.NotAfter) {
		report.Health = HealthExpiringSoon
	} else {
		report.Health = HealthUsable
	}
	if repairPermissions {
		if err := repairAuthorityPermissions(dir, authority); err != nil {
			report.Health = HealthInvalid
			return report, nil, err
		}
	}
	return report, authority, nil
}

func createCandidate(dir string) (*Authority, error) {
	authoritiesDir := filepath.Join(dir, AuthoritiesDirName)
	if err := os.MkdirAll(authoritiesDir, 0o700); err != nil {
		return nil, err
	}
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
	fingerprint, err := SHA1Fingerprint(certPEM)
	if err != nil {
		return nil, err
	}
	generationDir := filepath.Join(authoritiesDir, fingerprint)
	if err := os.Mkdir(generationDir, 0o700); err != nil {
		return nil, err
	}
	certPath := filepath.Join(generationDir, CertFileName)
	keyPath := filepath.Join(generationDir, KeyFileName)
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
	return &Authority{
		CertPath: certPath,
		KeyPath:  keyPath,
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		cert:     template,
		key:      key,
	}, nil
}

func loadGeneration(dir, fingerprint string) (*Authority, error) {
	if !validFingerprint(fingerprint) {
		return nil, fmt.Errorf("active UserCA fingerprint is invalid")
	}
	generationDir := filepath.Join(dir, AuthoritiesDirName, fingerprint)
	certPath := filepath.Join(generationDir, CertFileName)
	keyPath := filepath.Join(generationDir, KeyFileName)
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

func readActiveFingerprint(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, ActiveFingerprintFileName))
	if err != nil {
		return "", err
	}
	fingerprint := strings.TrimSpace(string(data))
	if !validFingerprint(fingerprint) {
		return "", fmt.Errorf("active UserCA fingerprint is invalid")
	}
	return fingerprint, nil
}

func writeActiveFingerprint(dir, fingerprint string) (bool, error) {
	if !validFingerprint(fingerprint) {
		return false, fmt.Errorf("active UserCA fingerprint is invalid")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	temp, err := os.CreateTemp(dir, ".active-fingerprint-*")
	if err != nil {
		return false, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return false, err
	}
	if _, err := temp.WriteString(fingerprint + "\n"); err != nil {
		_ = temp.Close()
		return false, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return false, err
	}
	if err := temp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tempPath, filepath.Join(dir, ActiveFingerprintFileName)); err != nil {
		return false, err
	}
	if err := syncDirectory(dir); err != nil {
		return true, err
	}
	return true, nil
}

func writeDurableFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

var syncDirectory = syncDirectoryPlatform

func syncDirectoryPlatform(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func cleanupNonActiveLocked(ctx context.Context, dir string, store TrustStore) error {
	active, markerErr := readActiveFingerprint(dir)
	if markerErr != nil {
		// An invalid or absent marker names no Active generation. Explicit
		// lifecycle reconciliation may therefore remove every owned residue.
		active = ""
	}
	records, err := store.TrustedCertificates(ctx)
	if err != nil {
		return err
	}
	var remove []string
	for _, record := range records {
		if record.Fingerprint != active {
			remove = append(remove, record.Fingerprint)
		}
	}
	if len(remove) > 0 {
		if err := store.Remove(ctx, remove); err != nil {
			return err
		}
	}
	authoritiesDir := filepath.Join(dir, AuthoritiesDirName)
	entries, err := os.ReadDir(authoritiesDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == active {
			continue
		}
		if err := os.RemoveAll(filepath.Join(authoritiesDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func cleanupCandidate(ctx context.Context, dir string, store TrustStore, fingerprint string) error {
	return errors.Join(
		store.Remove(ctx, []string{fingerprint}),
		os.RemoveAll(filepath.Join(dir, AuthoritiesDirName, fingerprint)),
	)
}

func removeNonActivePrivateKeys(dir, active string) {
	entries, err := os.ReadDir(filepath.Join(dir, AuthoritiesDirName))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Name() == active {
			continue
		}
		_ = os.Remove(filepath.Join(dir, AuthoritiesDirName, entry.Name(), KeyFileName))
	}
}

func nonActiveCount(records []TrustedCertificate, active string) int {
	count := 0
	for _, record := range records {
		if record.Fingerprint != active {
			count++
		}
	}
	return count
}

func nonActiveResidueCount(dir, active string, records []TrustedCertificate) int {
	residue := map[string]bool{}
	for _, record := range records {
		if record.Fingerprint != active {
			residue[record.Fingerprint] = true
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, AuthoritiesDirName))
	if err == nil {
		for _, entry := range entries {
			if entry.Name() != active {
				residue[entry.Name()] = true
			}
		}
	}
	return len(residue)
}

func containsFingerprint(records []TrustedCertificate, fingerprint string) bool {
	for _, record := range records {
		if record.Fingerprint == fingerprint {
			return true
		}
	}
	return false
}

func fingerprints(records []TrustedCertificate) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.Fingerprint)
	}
	return out
}

func validFingerprint(value string) bool {
	if len(value) != sha1.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToUpper(value)
}

var chmod = os.Chmod

func repairAuthorityPermissions(dir string, authority *Authority) error {
	return errors.Join(
		chmod(dir, 0o700),
		chmod(filepath.Join(dir, AuthoritiesDirName), 0o700),
		chmod(filepath.Dir(authority.CertPath), 0o700),
		chmod(filepath.Join(dir, ActiveFingerprintFileName), 0o600),
		chmod(authority.CertPath, 0o600),
		chmod(authority.KeyPath, 0o600),
	)
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

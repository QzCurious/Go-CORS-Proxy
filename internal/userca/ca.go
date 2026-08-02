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
	"sync"
	"time"
)

const (
	commonName                = "seamless-cors Installed User CA"
	authoritiesDirName        = "authorities"
	activeFingerprintFileName = "active-fingerprint"
	certFileName              = "certificate.pem"
	keyFileName               = "private-key.pem"
	validity                  = 5 * 365 * 24 * time.Hour
	renewalWindow             = 30 * 24 * time.Hour
)

var (
	errInvalidActiveFingerprint = errors.New("active UserCA fingerprint is invalid")
	errInvalidAuthority         = errors.New("UserCA authority material is invalid")
	errMutationInProgress       = errors.New("UserCA mutation already in progress")
	readFile                    = os.ReadFile
)

// UserCA owns Installed User CA material, its storage layout, and current-user
// operating-system trust integration. It does not cache inspection results.
type UserCA struct {
	dir        string
	store      trustStore
	now        func() time.Time
	mutationMu sync.Mutex
}

// Snapshot is an immutable semantic observation of UserCA facts.
type Snapshot struct {
	usable      bool
	certificate tls.Certificate
	expiresAt   time.Time
	renewalDue  bool
}

// NewSnapshot returns a usable immutable UserCA observation. The zero value
// represents a UserCA that is not usable.
func NewSnapshot(certificate tls.Certificate, expiresAt time.Time, renewalDue bool) (Snapshot, error) {
	if len(certificate.Certificate) == 0 || certificate.PrivateKey == nil || expiresAt.IsZero() {
		return Snapshot{}, fmt.Errorf("usable UserCA snapshot requires certificate, private key, and expiry")
	}
	return Snapshot{
		usable:      true,
		certificate: cloneTLSCertificate(certificate),
		expiresAt:   expiresAt,
		renewalDue:  renewalDue,
	}, nil
}

func (s Snapshot) Usable() bool { return s.usable }

func (s Snapshot) TLSCertificate() (tls.Certificate, bool) {
	if !s.usable {
		return tls.Certificate{}, false
	}
	return cloneTLSCertificate(s.certificate), true
}

func (s Snapshot) ExpiresAt() time.Time { return s.expiresAt }

func (s Snapshot) RenewalDue() bool { return s.renewalDue }

type InstallResult struct {
	current Snapshot
	changed bool
}

// NewInstallResult returns an immutable UserCA installation result.
func NewInstallResult(current Snapshot, changed bool) InstallResult {
	return InstallResult{current: current, changed: changed}
}

func (r InstallResult) Current() Snapshot { return r.current }

func (r InstallResult) Changed() bool { return r.changed }

type UninstallResult struct {
	current Snapshot
	changed bool
}

// NewUninstallResult returns an immutable UserCA removal result.
func NewUninstallResult(current Snapshot, changed bool) UninstallResult {
	return UninstallResult{current: current, changed: changed}
}

func (r UninstallResult) Current() Snapshot { return r.current }

func (r UninstallResult) Changed() bool { return r.changed }

type authority struct {
	certPath string
	keyPath  string
	certPEM  []byte
	keyPEM   []byte
	cert     *x509.Certificate
}

type assessment struct {
	snapshot          Snapshot
	authority         *authority
	activeFingerprint string
	activeTrusted     bool
	ownedFacts        bool
	needsRotation     bool
}

// Open resolves the private default storage and trust integration without
// inspecting or mutating either.
func Open() (*UserCA, error) {
	dir, err := defaultDir()
	if err != nil {
		return nil, err
	}
	return openAt(dir, newTrustStore(), time.Now), nil
}

func openAt(dir string, store trustStore, now func() time.Time) *UserCA {
	if now == nil {
		now = time.Now
	}
	return &UserCA{dir: dir, store: store, now: now}
}

// Inspect freshly derives a semantic snapshot from the Active marker, owned
// immutable generations, and current-user OS trust.
func (u *UserCA) Inspect(ctx context.Context) (Snapshot, error) {
	state, err := u.assess(ctx, false)
	return state.snapshot, err
}

// Install repairs a valid Active authority in place or installs a fresh
// immutable generation. Renewal is explicit and occurs only through Install.
func (u *UserCA) Install(ctx context.Context) (InstallResult, error) {
	if !u.mutationMu.TryLock() {
		return InstallResult{}, errMutationInProgress
	}
	defer u.mutationMu.Unlock()
	before, err := u.assess(ctx, false)
	if err != nil {
		return InstallResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}

	if before.authority != nil && !before.needsRotation {
		changed := !before.snapshot.Usable() || authorityPermissionsNeedRepair(u.dir, before.authority)
		if !before.activeTrusted {
			if err := u.store.Trust(ctx, before.authority.certPEM); err != nil {
				return InstallResult{}, err
			}
		}
		if err := repairAuthorityPermissions(u.dir, before.authority); err != nil {
			return InstallResult{}, err
		}
		// Residue cleanup is private best effort when the marked authority can
		// remain Active; inability to remove it does not make that authority
		// unusable or leak a cleanup condition through the seam.
		_ = cleanupNonActive(ctx, u.dir, u.store, before.activeFingerprint)
		current, err := u.Inspect(ctx)
		if err != nil {
			return InstallResult{}, err
		}
		if !current.Usable() {
			return InstallResult{}, fmt.Errorf("installed UserCA is not usable after repair")
		}
		return InstallResult{current: current, changed: changed}, nil
	}

	// Before adding another trusted root, establish that no ambiguous or
	// retired owned authority remains. The marked authority may remain only
	// while rotating a valid renewal-due generation.
	if before.authority != nil {
		if err := cleanupNonActive(ctx, u.dir, u.store, before.activeFingerprint); err != nil {
			return InstallResult{}, err
		}
	} else {
		if err := uninstallAll(ctx, u.dir, u.store); err != nil {
			return InstallResult{}, err
		}
		clean, err := u.assess(ctx, false)
		if err != nil {
			return InstallResult{}, err
		}
		if clean.ownedFacts {
			return InstallResult{}, fmt.Errorf("ambiguous UserCA state could not be cleared")
		}
	}

	candidate, err := createCandidate(u.dir, u.now)
	if err != nil {
		return InstallResult{}, err
	}
	fingerprint, err := candidate.fingerprint()
	if err != nil {
		_ = os.RemoveAll(filepath.Dir(candidate.certPath))
		return InstallResult{}, err
	}
	if err := u.store.Trust(ctx, candidate.certPEM); err != nil {
		cleanupErr := cleanupCandidate(context.Background(), u.dir, u.store, fingerprint)
		return InstallResult{}, errors.Join(err, cleanupErr)
	}

	markerCommitted, markerErr := writeActiveFingerprint(u.dir, fingerprint)
	if markerErr != nil && !markerCommitted {
		cleanupErr := cleanupCandidate(context.Background(), u.dir, u.store, fingerprint)
		return InstallResult{}, errors.Join(markerErr, cleanupErr)
	}
	// The previous authority remains trusted until Gateway has adopted the
	// returned generation. A later lifecycle event privately removes it.
	current, inspectErr := u.Inspect(context.Background())
	if inspectErr != nil {
		return InstallResult{}, errors.Join(markerErr, inspectErr)
	}
	if !current.Usable() {
		return InstallResult{}, errors.Join(markerErr, fmt.Errorf("installed UserCA is not usable after commit"))
	}
	return InstallResult{current: current, changed: true}, markerErr
}

// Uninstall removes every owned authority from OS trust and local storage.
func (u *UserCA) Uninstall(ctx context.Context) (UninstallResult, error) {
	if !u.mutationMu.TryLock() {
		return UninstallResult{}, errMutationInProgress
	}
	defer u.mutationMu.Unlock()
	before, err := u.assess(ctx, false)
	if err != nil {
		return UninstallResult{}, err
	}
	if err := uninstallAll(ctx, u.dir, u.store); err != nil {
		return UninstallResult{}, err
	}
	after, err := u.assess(ctx, false)
	if err != nil {
		return UninstallResult{}, err
	}
	if after.ownedFacts || after.snapshot.Usable() {
		return UninstallResult{}, fmt.Errorf("UserCA uninstall is incomplete")
	}
	return UninstallResult{current: after.snapshot, changed: before.ownedFacts}, nil
}

func (u *UserCA) assess(ctx context.Context, repairPermissions bool) (assessment, error) {
	records, err := u.store.TrustedCertificates(ctx)
	if err != nil {
		return assessment{}, err
	}
	localFacts, err := hasOwnedGenerations(u.dir)
	if err != nil {
		return assessment{}, err
	}
	state := assessment{ownedFacts: localFacts || len(records) > 0}
	fingerprint, err := readActiveFingerprint(u.dir)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, errInvalidActiveFingerprint) {
			return state, nil
		}
		return assessment{}, err
	}
	state.activeFingerprint = fingerprint
	state.ownedFacts = true
	active, err := loadGeneration(u.dir, fingerprint)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, errInvalidAuthority) {
			return state, nil
		}
		return assessment{}, err
	}
	actualFingerprint, err := active.fingerprint()
	if err != nil || actualFingerprint != fingerprint {
		return state, nil
	}
	state.authority = active
	state.activeTrusted = containsFingerprint(records, fingerprint)
	state.needsRotation = u.now().Add(renewalWindow).After(active.cert.NotAfter)
	if !state.activeTrusted || !u.now().Before(active.cert.NotAfter) {
		return state, nil
	}
	if repairPermissions {
		if err := repairAuthorityPermissions(u.dir, active); err != nil {
			return assessment{}, err
		}
	}
	certificate, err := active.tlsCertificate()
	if err != nil {
		return state, nil
	}
	state.snapshot = Snapshot{
		usable:      true,
		certificate: certificate,
		expiresAt:   active.cert.NotAfter,
		renewalDue:  state.needsRotation,
	}
	return state, nil
}

func (a *authority) fingerprint() (string, error) {
	return sha1Fingerprint(a.certPEM)
}

func (a *authority) tlsCertificate() (tls.Certificate, error) {
	cert, err := tls.X509KeyPair(a.certPEM, a.keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, err
	}
	return cert, nil
}

func cloneTLSCertificate(source tls.Certificate) tls.Certificate {
	clone := source
	clone.Certificate = make([][]byte, len(source.Certificate))
	for index, der := range source.Certificate {
		clone.Certificate[index] = append([]byte(nil), der...)
	}
	clone.OCSPStaple = append([]byte(nil), source.OCSPStaple...)
	clone.SupportedSignatureAlgorithms = append([]tls.SignatureScheme(nil), source.SupportedSignatureAlgorithms...)
	clone.SignedCertificateTimestamps = make([][]byte, len(source.SignedCertificateTimestamps))
	for index, timestamp := range source.SignedCertificateTimestamps {
		clone.SignedCertificateTimestamps[index] = append([]byte(nil), timestamp...)
	}
	if key, ok := source.PrivateKey.(*rsa.PrivateKey); ok {
		if clonedKey, err := x509.ParsePKCS1PrivateKey(x509.MarshalPKCS1PrivateKey(key)); err == nil {
			clone.PrivateKey = clonedKey
		}
	}
	if len(clone.Certificate) > 0 {
		if leaf, err := x509.ParseCertificate(clone.Certificate[0]); err == nil {
			clone.Leaf = leaf
		}
	}
	return clone
}

func uninstallAll(ctx context.Context, dir string, store trustStore) error {
	records, trustErr := store.TrustedCertificates(ctx)
	var removeErr error
	if trustErr == nil {
		if owned := fingerprints(records); len(owned) > 0 {
			removeErr = store.Remove(ctx, owned)
		}
	}
	return errors.Join(trustErr, removeErr, os.RemoveAll(dir))
}

func hasOwnedGenerations(dir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(dir, activeFingerprintFileName)); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, authoritiesDirName))
	if err == nil {
		return len(entries) > 0, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func createCandidate(dir string, now func() time.Time) (*authority, error) {
	authoritiesDir := filepath.Join(dir, authoritiesDirName)
	if err := os.MkdirAll(authoritiesDir, 0o700); err != nil {
		return nil, err
	}
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
	return &authority{
		certPath: certPath,
		keyPath:  keyPath,
		certPEM:  certPEM,
		keyPEM:   keyPEM,
		cert:     template,
	}, nil
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

func readActiveFingerprint(dir string) (string, error) {
	data, err := readFile(filepath.Join(dir, activeFingerprintFileName))
	if err != nil {
		return "", err
	}
	fingerprint := strings.TrimSpace(string(data))
	if !validFingerprint(fingerprint) {
		return "", errInvalidActiveFingerprint
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
	if err := os.Rename(tempPath, filepath.Join(dir, activeFingerprintFileName)); err != nil {
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

func cleanupNonActive(ctx context.Context, dir string, store trustStore, active string) error {
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
	authoritiesDir := filepath.Join(dir, authoritiesDirName)
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
	return verifyNonActiveCleared(ctx, dir, store, active)
}

func verifyNonActiveCleared(ctx context.Context, dir string, store trustStore, active string) error {
	records, err := store.TrustedCertificates(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Fingerprint != active {
			return fmt.Errorf("non-active UserCA trust remains after cleanup")
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, authoritiesDirName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.Name() != active {
			return fmt.Errorf("non-active UserCA material remains after cleanup")
		}
	}
	return nil
}

func cleanupCandidate(ctx context.Context, dir string, store trustStore, fingerprint string) error {
	return errors.Join(
		store.Remove(ctx, []string{fingerprint}),
		os.RemoveAll(filepath.Join(dir, authoritiesDirName, fingerprint)),
	)
}

func containsFingerprint(records []trustedCertificate, fingerprint string) bool {
	for _, record := range records {
		if record.Fingerprint == fingerprint {
			return true
		}
	}
	return false
}

func fingerprints(records []trustedCertificate) []string {
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

func authorityPermissionsNeedRepair(dir string, authority *authority) bool {
	expected := map[string]os.FileMode{
		dir:                                    0o700,
		filepath.Join(dir, authoritiesDirName): 0o700,
		filepath.Dir(authority.certPath):       0o700,
		filepath.Join(dir, activeFingerprintFileName): 0o600,
		authority.certPath:                            0o600,
		authority.keyPath:                             0o600,
	}
	for path, mode := range expected {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			return true
		}
	}
	return false
}

func repairAuthorityPermissions(dir string, authority *authority) error {
	return errors.Join(
		chmod(dir, 0o700),
		chmod(filepath.Join(dir, authoritiesDirName), 0o700),
		chmod(filepath.Dir(authority.certPath), 0o700),
		chmod(filepath.Join(dir, activeFingerprintFileName), 0o600),
		chmod(authority.certPath, 0o600),
		chmod(authority.keyPath, 0o600),
	)
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
	return &authority{
		certPath: certPath,
		keyPath:  keyPath,
		certPEM:  certPEM,
		keyPEM:   keyPEM,
		cert:     cert,
	}, nil
}

func sha1Fingerprint(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("CA certificate PEM is invalid")
	}
	sum := sha1.Sum(block.Bytes)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

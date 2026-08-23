package userca

import (
	"crypto/tls"
	"errors"
	"fmt"
	"os"
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
	renewalWindow             = 90 * 24 * time.Hour
)

var (
	errInvalidActiveFingerprint = errors.New("active UserCA fingerprint is invalid")
	errInvalidAuthority         = errors.New("UserCA authority material is invalid")
	errMutationInProgress       = errors.New("UserCA mutation already in progress")
	readFile                    = os.ReadFile
)

// CA maintains the current user's seamless-cors development authority as one
// coherent capability. It does not cache assessment results.
type CA struct {
	dir        string
	store      trustStore
	now        func() time.Time
	mutationMu sync.Mutex
}

// Snapshot is an immutable semantic observation of UserCA facts.
type Snapshot struct {
	usable     bool
	expiresAt  time.Time
	renewalDue bool
}

// newSnapshot returns a usable immutable UserCA observation. The zero value
// represents a UserCA that is not usable.
func newSnapshot(expiresAt time.Time, renewalDue bool) (Snapshot, error) {
	if expiresAt.IsZero() {
		return Snapshot{}, fmt.Errorf("usable UserCA snapshot requires expiry")
	}
	return Snapshot{usable: true, expiresAt: expiresAt, renewalDue: renewalDue}, nil
}

func (s Snapshot) Usable() bool { return s.usable }

func (s Snapshot) ExpiresAt() time.Time { return s.expiresAt }

func (s Snapshot) RenewalDue() bool { return s.renewalDue }

// Assessment is one coherent UserCA observation and its matching immutable
// signing material. The zero value is not usable; a usable value always has
// signing material.
type Assessment struct {
	snapshot    Snapshot
	certificate *tls.Certificate
}

// NewAssessment constructs a usable assessment for adapters at the UserCA
// seam. The production module additionally validates the authority material.
func NewAssessment(expiresAt time.Time, renewalDue bool, certificate *tls.Certificate) (Assessment, error) {
	if certificate == nil {
		return Assessment{}, fmt.Errorf("usable UserCA assessment requires signing material")
	}
	snapshot, err := newSnapshot(expiresAt, renewalDue)
	if err != nil {
		return Assessment{}, err
	}
	return Assessment{snapshot: snapshot, certificate: certificate}, nil
}

func (a Assessment) Snapshot() Snapshot { return a.snapshot }

// SigningMaterial returns nil exactly when the assessment is not usable.
func (a Assessment) SigningMaterial() *tls.Certificate { return a.certificate }

// MutationResult reports the coherent postcondition of an install or uninstall.
type MutationResult struct {
	current Assessment
	changed bool
}

// NewMutationResult constructs a mutation result for adapters at the UserCA
// seam. Production results are created by Install and Uninstall.
func NewMutationResult(current Assessment, changed bool) MutationResult {
	return MutationResult{current: current, changed: changed}
}

func (r MutationResult) Current() Assessment { return r.current }

func (r MutationResult) Changed() bool { return r.changed }

// Open resolves private storage and platform trust integration without
// inspecting or mutating either.
func Open() (*CA, error) {
	dir, err := defaultDir()
	if err != nil {
		return nil, err
	}
	return openAt(dir, newTrustStore(), time.Now), nil
}

func openAt(dir string, store trustStore, now func() time.Time) *CA {
	if now == nil {
		now = time.Now
	}
	return &CA{dir: dir, store: store, now: now}
}

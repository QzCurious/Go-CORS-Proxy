package userca

import (
	"crypto/tls"
	"fmt"
	"time"
)

// CurrentState is one coherent observation of UserCA facts and, exactly when
// usable, the matching opaque signing capability. Its zero value is not usable.
type CurrentState struct {
	Usable     bool
	ExpiresAt  time.Time
	RenewalDue bool

	signingMaterial *tls.Certificate
}

// NewCurrentState constructs usable state for adapters at the UserCA seam.
// The production module additionally validates the authority material.
func NewCurrentState(expiresAt time.Time, renewalDue bool, signingMaterial *tls.Certificate) (CurrentState, error) {
	if expiresAt.IsZero() {
		return CurrentState{}, fmt.Errorf("usable UserCA current state requires expiry")
	}
	if signingMaterial == nil {
		return CurrentState{}, fmt.Errorf("usable UserCA current state requires signing material")
	}
	return CurrentState{
		Usable:          true,
		ExpiresAt:       expiresAt,
		RenewalDue:      renewalDue,
		signingMaterial: signingMaterial,
	}, nil
}

// SigningMaterial returns nil exactly when the current state is not usable.
func (s CurrentState) SigningMaterial() *tls.Certificate { return s.signingMaterial }

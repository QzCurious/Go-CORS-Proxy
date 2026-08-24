package userca

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"time"
)

const renewalWindow = 90 * 24 * time.Hour

// inspectedState carries private reconciliation facts alongside State.
type inspectedState struct {
	current        State
	authority      *authority
	trusted        bool
	ownedFacts     bool
	renewalDue     bool
	authorityValid bool
}

func (u *CA) inspect(ctx context.Context) (inspectedState, error) {
	// Discover every owned trust and storage fact.
	records, err := u.store.TrustedCertificates(ctx)
	if err != nil {
		return inspectedState{}, err
	}
	localFacts, err := hasOwnedMaterial(u.dir)
	if err != nil {
		return inspectedState{}, err
	}
	state := inspectedState{ownedFacts: localFacts || len(records) > 0}

	// Load the one locally published authority pair.
	active, err := loadAuthority(u.dir)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, errInvalidAuthority) {
			return state, nil
		}
		return inspectedState{}, err
	}
	fingerprint, err := active.fingerprint()
	if err != nil {
		return state, nil
	}
	state.ownedFacts = true

	// Establish validity, trust, and renewal facts.
	state.authority = active
	state.trusted = containsFingerprint(records, fingerprint)
	state.renewalDue = !u.now().Add(renewalWindow).Before(active.cert.NotAfter)
	certificate, err := active.tlsCertificate()
	if err != nil {
		return state, nil
	}
	material, err := validateSigningMaterial(certificate, u.now())
	if err != nil {
		return state, nil
	}
	state.authorityValid = true
	if !state.trusted {
		return state, nil
	}

	// Publish one coherent usable current state.
	state.current, err = newState(active.cert.NotAfter, state.renewalDue, material)
	if err != nil {
		return inspectedState{}, err
	}
	return state, nil
}

func newState(expiresAt time.Time, renewalDue bool, signingMaterial *tls.Certificate) (State, error) {
	if expiresAt.IsZero() {
		return State{}, fmt.Errorf("usable UserCA current state requires expiry")
	}
	if signingMaterial == nil {
		return State{}, fmt.Errorf("usable UserCA current state requires signing material")
	}
	return State{
		Usable:          true,
		ExpiresAt:       expiresAt,
		RenewalDue:      renewalDue,
		SigningMaterial: signingMaterial,
	}, nil
}

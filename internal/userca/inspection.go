package userca

import (
	"context"
	"errors"
	"io/fs"
	"time"
)

const renewalWindow = 90 * 24 * time.Hour

// inspectedState carries private reconciliation facts alongside State.
type inspectedState struct {
	current        State
	authority      *authority
	trusted        bool
	ownedTrust     bool
	ownedFacts     bool
	renewalDue     bool
	authorityValid bool
}

func (u *CA) inspect(ctx context.Context) (inspectedState, error) {
	// Discover every owned trust and storage fact.
	records, err := u.trustStore.List(ctx)
	if err != nil {
		return inspectedState{}, err
	}
	owned := ownedCertificates(records)
	localFacts, err := hasOwnedMaterial(u.dir)
	if err != nil {
		return inspectedState{}, err
	}
	state := inspectedState{ownedTrust: len(owned) > 0, ownedFacts: localFacts || len(owned) > 0}

	// Load the one locally published authority pair.
	active, err := loadAuthority(u.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errInvalidAuthority) {
			return state, nil
		}
		return inspectedState{}, err
	}
	state.ownedFacts = true

	// Validate ownership and the validity period before deriving usability facts.
	now := u.now()
	if !isOwnedAuthorityCertificate(active.cert) ||
		now.Before(active.cert.NotBefore) ||
		!now.Before(active.cert.NotAfter) {
		return state, nil
	}
	state.authority = active
	state.authorityValid = true
	state.trusted = len(owned) == 1 && owned[0].X509.Equal(active.cert)
	state.renewalDue = !now.Add(renewalWindow).Before(active.cert.NotAfter)
	if !state.trusted {
		return state, nil
	}

	// Publish one coherent usable current state.
	material := active.tlsCertificate()
	state.current = State{
		Usable:          true,
		ExpiresAt:       active.cert.NotAfter,
		RenewalDue:      state.renewalDue,
		SigningMaterial: &material,
	}
	return state, nil
}

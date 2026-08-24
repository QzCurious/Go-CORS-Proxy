package userca

import (
	"context"
	"errors"
	"os"
)

// inspectedState carries private reconciliation facts alongside CurrentState.
type inspectedState struct {
	current        CurrentState
	authority      *authority
	trusted        bool
	ownedFacts     bool
	renewalDue     bool
	authorityValid bool
}

// Inspect freshly derives one coherent CurrentState from the local pair and
// current-user OS trust.
func (u *CA) Inspect(ctx context.Context) (CurrentState, error) {
	state, err := u.inspect(ctx)
	return state.current, err
}

func (u *CA) inspect(ctx context.Context) (inspectedState, error) {
	// Phase 1: discover every owned trust and storage fact.
	records, err := u.store.TrustedCertificates(ctx)
	if err != nil {
		return inspectedState{}, err
	}
	localFacts, err := hasOwnedMaterial(u.dir)
	if err != nil {
		return inspectedState{}, err
	}
	state := inspectedState{ownedFacts: localFacts || len(records) > 0}

	// Phase 2: load the one locally published authority pair.
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

	// Phase 3: establish validity, trust, and renewal facts.
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

	// Phase 4: publish one coherent usable current state.
	state.current, err = NewCurrentState(active.cert.NotAfter, state.renewalDue, material)
	if err != nil {
		return inspectedState{}, err
	}
	return state, nil
}

package userca

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
)

// assessment carries private recovery facts alongside the public postcondition.
type assessment struct {
	snapshot          Snapshot
	certificate       *tls.Certificate
	authority         *authority
	activeFingerprint string
	activeTrusted     bool
	ownedFacts        bool
	needsRotation     bool
}

// Inspect freshly derives one coherent assessment from local generations, the
// Active marker, and current-user OS trust.
func (u *CA) Inspect(ctx context.Context) (Assessment, error) {
	state, err := u.assess(ctx, false)
	return Assessment{snapshot: state.snapshot, certificate: state.certificate}, err
}

func (u *CA) assess(ctx context.Context, repairPermissions bool) (assessment, error) {
	return u.assessInternal(ctx, repairPermissions, true)
}

func (u *CA) assessForRemoval(ctx context.Context) (assessment, error) {
	return u.assessInternal(ctx, false, false)
}

func (u *CA) assessInternal(ctx context.Context, repairPermissions, requireCertificate bool) (assessment, error) {
	// Phase 1: discover every owned trust and storage fact.
	records, err := u.store.TrustedCertificates(ctx)
	if err != nil {
		return assessment{}, err
	}
	localFacts, err := hasOwnedGenerations(u.dir)
	if err != nil {
		return assessment{}, err
	}
	state := assessment{ownedFacts: localFacts || len(records) > 0}

	// Phase 2: resolve only the generation named by the durable Active marker.
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

	// Phase 3: establish trust, validity, renewal, and permission facts.
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

	// Phase 4: validate and publish one coherent usable capability.
	certificate, err := active.tlsCertificate()
	if err != nil {
		return state, nil
	}
	material, err := validateSigningMaterial(certificate, u.now())
	if err != nil {
		if !requireCertificate {
			return state, nil
		}
		return assessment{}, fmt.Errorf("assess UserCA signing material: %w", err)
	}
	state.snapshot, err = newSnapshot(active.cert.NotAfter, state.needsRotation)
	if err != nil {
		return assessment{}, err
	}
	state.certificate = material
	return state, nil
}

func (u *CA) signingMaterialForAuthority(active *authority) (*tls.Certificate, error) {
	certificate, err := active.tlsCertificate()
	if err != nil {
		return nil, fmt.Errorf("UserCA signing material is invalid: %w", err)
	}
	return validateSigningMaterial(certificate, u.now())
}

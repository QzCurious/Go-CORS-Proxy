package userca

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Install establishes a usable authority, repairing or renewing the Active
// generation when possible. Renewal occurs only through this explicit mutation.
func (u *CA) Install(ctx context.Context) (MutationResult, error) {
	// Phase 1: admit one mutation and assess the precondition once.
	if !u.mutationMu.TryLock() {
		return MutationResult{}, errMutationInProgress
	}
	defer u.mutationMu.Unlock()
	before, err := u.assess(ctx, false)
	if err != nil {
		return MutationResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return MutationResult{}, err
	}

	// Phase 2: reuse a valid Active generation, repairing trust and permissions.
	if before.authority != nil && !before.needsRotation {
		return u.reuseActive(ctx, before)
	}

	// Phase 3: prove that adding one Candidate cannot accumulate ambiguous roots.
	if err := u.prepareForCandidate(ctx, before); err != nil {
		return MutationResult{}, err
	}

	// Phase 4: create and validate an immutable local Candidate.
	candidate, err := createCandidate(u.dir, u.now)
	if err != nil {
		return MutationResult{}, err
	}
	fingerprint, err := candidate.fingerprint()
	if err != nil {
		_ = os.RemoveAll(filepath.Dir(candidate.certPath))
		return MutationResult{}, err
	}
	certificate, err := u.signingMaterialForAuthority(candidate)
	if err != nil {
		_ = os.RemoveAll(filepath.Dir(candidate.certPath))
		return MutationResult{}, err
	}

	// Phase 5: trust the Candidate before it can become Active.
	if err := u.store.Trust(ctx, candidate.certPEM); err != nil {
		cleanupErr := cleanupCandidate(context.Background(), u.dir, u.store, fingerprint)
		return MutationResult{}, errors.Join(err, cleanupErr)
	}

	// Phase 6: atomically commit Active, then return the committed capability.
	markerCommitted, markerErr := writeActiveFingerprint(u.dir, fingerprint)
	if markerErr != nil && !markerCommitted {
		cleanupErr := cleanupCandidate(context.Background(), u.dir, u.store, fingerprint)
		return MutationResult{}, errors.Join(markerErr, cleanupErr)
	}
	current, assessmentErr := NewAssessment(candidate.cert.NotAfter, false, certificate)
	if assessmentErr != nil {
		return MutationResult{}, errors.Join(markerErr, assessmentErr)
	}
	// Retired trust may overlap until Gateway adopts this returned capability;
	// the next lifecycle mutation privately retries its cleanup.
	return MutationResult{current: current, changed: true}, markerErr
}

func (u *CA) reuseActive(ctx context.Context, before assessment) (MutationResult, error) {
	certificate, err := u.signingMaterialForAuthority(before.authority)
	if err != nil {
		return MutationResult{}, err
	}
	changed := !before.snapshot.Usable() || authorityPermissionsNeedRepair(u.dir, before.authority)
	if !before.activeTrusted {
		if err := u.store.Trust(ctx, before.authority.certPEM); err != nil {
			return MutationResult{}, err
		}
	}
	if err := repairAuthorityPermissions(u.dir, before.authority); err != nil {
		return MutationResult{}, err
	}
	// Residue cleanup is best effort because it cannot invalidate this Active
	// authority and must not leak a private cleanup condition through the seam.
	_ = cleanupNonActive(ctx, u.dir, u.store, before.activeFingerprint)
	current, err := NewAssessment(before.authority.cert.NotAfter, before.needsRotation, certificate)
	if err != nil {
		return MutationResult{}, err
	}
	return MutationResult{current: current, changed: changed}, nil
}

func (u *CA) prepareForCandidate(ctx context.Context, before assessment) error {
	if before.authority != nil {
		return cleanupNonActive(ctx, u.dir, u.store, before.activeFingerprint)
	}
	if err := uninstallAll(ctx, u.dir, u.store); err != nil {
		return err
	}
	clean, err := u.assess(ctx, false)
	if err != nil {
		return err
	}
	if clean.ownedFacts {
		return fmt.Errorf("ambiguous UserCA state could not be cleared")
	}
	return nil
}

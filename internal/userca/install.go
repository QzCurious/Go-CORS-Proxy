package userca

import (
	"context"
	"errors"
	"fmt"
)

// Install establishes one usable authority pair. A valid pair is reused and
// repaired; renewal, expiry, and invalid or ambiguous state are replaced.
// Gateway serializes this mutation and owns its runtime consequences.
func (u *CA) Install(ctx context.Context) (MutationResult, error) {
	// Phase 1: assess the precondition once.
	before, err := u.inspect(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return MutationResult{}, err
	}

	// Phase 2: reuse valid material that is not due for renewal, repairing its
	// current-user trust and local permissions in place.
	if before.authorityValid && !before.renewalDue {
		return u.reuseAuthority(ctx, before)
	}

	// Phase 3: remove and verify every owned fact before replacement. This
	// deliberately accepts a short explicit-install interruption instead of
	// maintaining overlapping authority generations.
	if err := uninstallAll(ctx, u.dir, u.store); err != nil {
		return MutationResult{}, err
	}
	clean, err := u.inspect(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	if clean.ownedFacts {
		return MutationResult{}, fmt.Errorf("owned UserCA state could not be cleared")
	}

	// Phase 4: atomically publish one complete local pair.
	authority, err := createAuthority(u.dir, u.now)
	if err != nil {
		return MutationResult{}, err
	}

	// Phase 5: trust the published pair, cleaning the recoverable footprint if
	// platform approval or trust mutation fails.
	if err := u.store.Trust(ctx, authority.certPEM); err != nil {
		cleanupErr := uninstallAll(context.Background(), u.dir, u.store)
		return MutationResult{}, errors.Join(err, cleanupErr)
	}

	// Phase 6: return only a freshly verified coherent postcondition.
	current, err := u.Inspect(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	if !current.Usable {
		return MutationResult{}, fmt.Errorf("installed UserCA is not usable")
	}
	return MutationResult{current: current, changed: true}, nil
}

func (u *CA) reuseAuthority(ctx context.Context, before inspectedState) (MutationResult, error) {
	changed := !before.current.Usable || authorityPermissionsNeedRepair(u.dir, before.authority)
	if !before.trusted {
		if err := u.store.Trust(ctx, before.authority.certPEM); err != nil {
			return MutationResult{}, err
		}
	}
	if err := repairAuthorityPermissions(u.dir, before.authority); err != nil {
		return MutationResult{}, err
	}
	current, err := u.Inspect(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	if !current.Usable {
		return MutationResult{}, fmt.Errorf("repaired UserCA is not usable")
	}
	return MutationResult{current: current, changed: changed}, nil
}

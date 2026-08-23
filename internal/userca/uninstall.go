package userca

import (
	"context"
	"fmt"
)

// Uninstall removes and verifies every owned trust and local authority fact.
func (u *CA) Uninstall(ctx context.Context) (MutationResult, error) {
	// Phase 1: admit one mutation and record whether anything is owned.
	if !u.mutationMu.TryLock() {
		return MutationResult{}, errMutationInProgress
	}
	defer u.mutationMu.Unlock()
	before, err := u.assessForRemoval(ctx)
	if err != nil {
		return MutationResult{}, err
	}

	// Phase 2: remove all strict-footprint OS trust and local storage.
	if err := uninstallAll(ctx, u.dir, u.store); err != nil {
		return MutationResult{}, err
	}

	// Phase 3: verify the absent postcondition before reporting success.
	after, err := u.assessForRemoval(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	if after.ownedFacts || after.snapshot.Usable() {
		return MutationResult{}, fmt.Errorf("UserCA uninstall is incomplete")
	}
	return MutationResult{current: Assessment{}, changed: before.ownedFacts}, nil
}

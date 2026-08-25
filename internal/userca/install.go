package userca

import (
	"context"
	"errors"
	"fmt"
)

func (u *CA) install(ctx context.Context) (State, error) {
	before, err := u.inspect(ctx)
	if err != nil {
		return State{}, err
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}

	if before.authorityValid && !before.renewalDue && (before.trusted || !before.ownedTrust) {
		return u.reuseAuthority(ctx, before)
	}

	// Replacement deliberately accepts a short explicit-install interruption
	// instead of maintaining overlapping authorities.
	if err := uninstallAll(ctx, u.dir, u.trustStore); err != nil {
		return State{}, err
	}
	clean, err := u.inspect(ctx)
	if err != nil {
		return State{}, err
	}
	if clean.ownedFacts {
		return State{}, fmt.Errorf("owned UserCA state could not be cleared")
	}

	authority, err := createAuthority(u.dir, u.now)
	if err != nil {
		return State{}, err
	}

	// A failed trust mutation must not leave an untrusted local pair behind.
	if err := u.trustStore.Add(ctx, authority.certPath); err != nil {
		cleanupErr := uninstallAll(context.Background(), u.dir, u.trustStore)
		return State{}, errors.Join(err, cleanupErr)
	}

	current, err := u.Inspect(ctx)
	if err != nil {
		return State{}, err
	}
	if !current.Usable {
		return State{}, fmt.Errorf("installed UserCA is not usable")
	}
	return current, nil
}

func (u *CA) reuseAuthority(ctx context.Context, before inspectedState) (State, error) {
	if !before.trusted {
		if err := u.trustStore.Add(ctx, before.authority.certPath); err != nil {
			return State{}, err
		}
	}
	if err := repairAuthorityPermissions(u.dir, before.authority); err != nil {
		return State{}, err
	}
	current, err := u.Inspect(ctx)
	if err != nil {
		return State{}, err
	}
	if !current.Usable {
		return State{}, fmt.Errorf("repaired UserCA is not usable")
	}
	return current, nil
}

package userca

import (
	"context"
	"fmt"
)

func (u *CA) uninstall(ctx context.Context) error {
	if _, err := u.inspect(ctx); err != nil {
		return err
	}

	if err := uninstallAll(ctx, u.dir, u.trustStore); err != nil {
		return err
	}

	// Verify the absent postcondition before reporting success.
	after, err := u.inspect(ctx)
	if err != nil {
		return err
	}
	if after.ownedFacts || after.current.Usable {
		return fmt.Errorf("UserCA uninstall is incomplete")
	}
	return nil
}

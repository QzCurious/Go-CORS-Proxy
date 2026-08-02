package gateway

import (
	"context"

	"github.com/QzCurious/seamless-cors/internal/userca"
)

// userCAModule is the Gateway-owned behavioral seam. UserCA owns every
// semantic value crossing it.
type userCAModule interface {
	Inspect(context.Context) (userca.Snapshot, error)
	Install(context.Context) (userca.InstallResult, error)
	Uninstall(context.Context) (userca.UninstallResult, error)
}

func openSystemUserCA() (userCAModule, error) {
	return userca.Open()
}

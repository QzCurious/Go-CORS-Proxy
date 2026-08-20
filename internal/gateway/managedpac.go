package gateway

import (
	"context"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

// managedPACModule is the Gateway-owned behavioral seam. Managed PAC owns
// every semantic value crossing it; platform settings and mutation
// serialization remain private to that feature.
type managedPACModule interface {
	Inspect(context.Context) (managedpac.Snapshot, error)
	InstallProjection(context.Context, []string, string, string) (managedpac.InstallResult, error)
	PublishProjection(string)
	Uninstall(context.Context) error
}

func openSystemManagedPAC() managedPACModule {
	return managedpac.Open()
}

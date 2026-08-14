package gateway

import (
	"context"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/pacrouting"
)

// managedPACModule is the Gateway-owned behavioral seam. Managed PAC owns
// every semantic value crossing it; platform settings and mutation
// serialization remain private to that feature.
type managedPACModule interface {
	Inspect(context.Context) (managedpac.Snapshot, error)
	InstallProjection(context.Context, []string, pacrouting.Projection) (managedpac.InstallResult, error)
	PublishProjection(pacrouting.Projection)
	Uninstall(context.Context) error
}

func openSystemManagedPAC() managedPACModule {
	return managedpac.Open()
}

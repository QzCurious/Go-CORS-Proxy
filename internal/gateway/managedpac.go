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
	Install(context.Context, []string, string) (managedpac.InstallResult, error)
	Deliver()
	RoutingReady(context.Context, string) (bool, []managedpac.ObservationIssue, error)
	ReconciliationResults() <-chan managedpac.ReconciliationResult
	CleanupActiveState(context.Context) (managedpac.CleanupResult, error)
	Uninstall(context.Context) (managedpac.CleanupResult, error)
}

func openSystemManagedPAC() managedPACModule {
	return managedpac.Open()
}

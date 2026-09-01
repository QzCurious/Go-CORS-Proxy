package gateway

import "github.com/QzCurious/seamless-cors/internal/managedpac"

// managedPACCapabilities is used only at Gateway composition points that need
// both Managed PAC seams. Lifecycle code retains and passes the narrower
// activation, control, or footprint capability for each workflow.
type managedPACCapabilities interface {
	managedpac.Activation
	managedpac.Footprint
}

func openSystemManagedPAC() managedPACCapabilities {
	return managedpac.New()
}

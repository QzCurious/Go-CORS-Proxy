package gateway

import (
	"context"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

type managedPACOwnership string

const (
	managedPACOwnershipEmpty   managedPACOwnership = "empty"
	managedPACOwnershipOwned   managedPACOwnership = "owned"
	managedPACOwnershipForeign managedPACOwnership = "foreign"
)

type managedPACService struct {
	name      string
	enabled   bool
	url       string
	ownership managedPACOwnership
}

func (s managedPACService) manageable() bool {
	return s.ownership != managedPACOwnershipForeign
}

type managedPACSnapshot struct {
	services []managedPACService
}

func (s managedPACSnapshot) manageableServices() []string {
	var services []string
	for _, service := range s.services {
		if service.manageable() {
			services = append(services, service.name)
		}
	}
	return services
}

func (s managedPACSnapshot) hasOwnedState() bool {
	for _, service := range s.services {
		if service.ownership == managedPACOwnershipOwned {
			return true
		}
	}
	return false
}

type managedPACRuntimeState struct {
	services  []string
	pacURL    string
	moduleRaw managedpac.RuntimeState
}

type managedPACWarningKind string

const (
	managedPACWarningDrift        managedPACWarningKind = "drift"
	managedPACWarningUpdateFailed managedPACWarningKind = "update-failed"
)

type managedPACWarning struct {
	kind        managedPACWarningKind
	serviceName string
	diagnostic  string
}

type managedPACInstallResult struct {
	state             managedPACRuntimeState
	installedServices []string
	warnings          []managedPACWarning
}

type managedPACReconcileResult struct {
	warnings []managedPACWarning
	err      error
}

// managedPACModule is the Gateway-owned seam. Platform settings and mutation
// serialization remain behind the Managed PAC module.
type managedPACModule interface {
	Inspect(context.Context) (managedPACSnapshot, error)
	Install(context.Context, []string, string) (managedPACInstallResult, error)
	RequestReconcile(managedPACRuntimeState, string, func(managedPACReconcileResult))
	Uninstall(context.Context) error
}

type systemManagedPAC struct {
	module *managedpac.ManagedPAC
}

func openSystemManagedPAC() managedPACModule {
	return systemManagedPAC{module: managedpac.Open()}
}

func (m systemManagedPAC) Inspect(ctx context.Context) (managedPACSnapshot, error) {
	snapshot, err := m.module.Inspect(ctx)
	if err != nil {
		return managedPACSnapshot{}, err
	}
	services := make([]managedPACService, 0, len(snapshot.Services()))
	for _, service := range snapshot.Services() {
		services = append(services, managedPACService{
			name:      service.Name,
			enabled:   service.Enabled,
			url:       service.URL,
			ownership: adaptManagedPACOwnership(service.Ownership),
		})
	}
	return managedPACSnapshot{services: services}, nil
}

func (m systemManagedPAC) Install(ctx context.Context, services []string, pacURL string) (managedPACInstallResult, error) {
	result, err := m.module.Install(ctx, services, pacURL)
	return managedPACInstallResult{
		state: managedPACRuntimeState{
			services:  result.State().ServiceNames(),
			pacURL:    result.State().PACURL(),
			moduleRaw: result.State(),
		},
		installedServices: result.InstalledServices(),
		warnings:          adaptManagedPACWarnings(result.Warnings()),
	}, err
}

func (m systemManagedPAC) RequestReconcile(state managedPACRuntimeState, pacURL string, complete func(managedPACReconcileResult)) {
	m.module.RequestReconcile(state.moduleRaw, pacURL, func(result managedpac.ReconcileResult) {
		if complete != nil {
			complete(managedPACReconcileResult{
				warnings: adaptManagedPACWarnings(result.Warnings()),
				err:      result.Err(),
			})
		}
	})
}

func (m systemManagedPAC) Uninstall(ctx context.Context) error {
	return m.module.Uninstall(ctx)
}

func adaptManagedPACOwnership(ownership managedpac.Ownership) managedPACOwnership {
	switch ownership {
	case managedpac.OwnershipEmpty:
		return managedPACOwnershipEmpty
	case managedpac.OwnershipOwned:
		return managedPACOwnershipOwned
	default:
		return managedPACOwnershipForeign
	}
}

func adaptManagedPACWarnings(warnings []managedpac.Warning) []managedPACWarning {
	out := make([]managedPACWarning, 0, len(warnings))
	for _, warning := range warnings {
		kind := managedPACWarningUpdateFailed
		if warning.Kind == managedpac.WarningDrift {
			kind = managedPACWarningDrift
		}
		out = append(out, managedPACWarning{
			kind:        kind,
			serviceName: warning.ServiceName,
			diagnostic:  warning.Diagnostic,
		})
	}
	return out
}

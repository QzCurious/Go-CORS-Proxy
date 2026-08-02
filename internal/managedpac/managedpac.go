package managedpac

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var errNoServicesInstalled = errors.New("managed PAC install updated no services")

type Ownership string

const (
	OwnershipEmpty   Ownership = "empty"
	OwnershipOwned   Ownership = "owned"
	OwnershipForeign Ownership = "foreign"
)

type Service struct {
	Name      string
	Enabled   bool
	URL       string
	Ownership Ownership
}

func (s Service) Manageable() bool { return s.Ownership != OwnershipForeign }

type Snapshot struct {
	services []Service
}

func (s Snapshot) Services() []Service {
	return append([]Service(nil), s.services...)
}

func (s Snapshot) ManageableServices() []string {
	var names []string
	for _, service := range s.services {
		if service.Manageable() {
			names = append(names, service.Name)
		}
	}
	return names
}

func (s Snapshot) HasOwnedState() bool {
	for _, service := range s.services {
		if service.Ownership == OwnershipOwned {
			return true
		}
	}
	return false
}

type RuntimeState struct {
	serviceNames []string
	pacURL       string
}

func (s RuntimeState) ServiceNames() []string {
	return append([]string(nil), s.serviceNames...)
}

func (s RuntimeState) PACURL() string { return s.pacURL }

type WarningKind string

const (
	WarningDrift        WarningKind = "drift"
	WarningUpdateFailed WarningKind = "update-failed"
)

type Warning struct {
	Kind        WarningKind
	ServiceName string
	Diagnostic  string
}

type InstallResult struct {
	state             RuntimeState
	installedServices []string
	warnings          []Warning
}

func (r InstallResult) State() RuntimeState { return cloneRuntimeState(r.state) }

func (r InstallResult) InstalledServices() []string {
	return append([]string(nil), r.installedServices...)
}

func (r InstallResult) Warnings() []Warning {
	return append([]Warning(nil), r.warnings...)
}

type ReconcileResult struct {
	warnings []Warning
	err      error
}

func (r ReconcileResult) Warnings() []Warning {
	return append([]Warning(nil), r.warnings...)
}

func (r ReconcileResult) Err() error { return r.err }

type reconcileRequest struct {
	generation uint64
	state      RuntimeState
	pacURL     string
	complete   func(ReconcileResult)
}

// ManagedPAC owns platform PAC inspection, mutation serialization, request
// preemption, and complete marker-based teardown behind one semantic interface.
type ManagedPAC struct {
	settings systemSettings

	opMu sync.Mutex
	mu   sync.Mutex

	accepting    bool
	generation   uint64
	pending      *reconcileRequest
	activeCancel context.CancelFunc
	worker       bool
}

func Open() *ManagedPAC {
	return openWithSettings(newSystemSettings())
}

func openWithSettings(settings systemSettings) *ManagedPAC {
	return &ManagedPAC{settings: settings}
}

func (m *ManagedPAC) Inspect(ctx context.Context) (Snapshot, error) {
	states, err := m.settings.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("managed PAC inspection failed: %w", err)
	}
	services := make([]Service, 0, len(states))
	for _, state := range states {
		services = append(services, Service{
			Name:      state.ServiceName,
			Enabled:   state.Enabled,
			URL:       state.PACURL,
			Ownership: ownershipForURL(state.PACURL),
		})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return Snapshot{services: services}, nil
}

func ownershipForURL(raw string) Ownership {
	if raw == "" || raw == "(null)" {
		return OwnershipEmpty
	}
	if isOwnedURL(raw) {
		return OwnershipOwned
	}
	return OwnershipForeign
}

func (m *ManagedPAC) Install(ctx context.Context, serviceNames []string, pacURL string) (InstallResult, error) {
	selected := sortedUniqueStrings(serviceNames)
	if len(selected) == 0 {
		return InstallResult{}, fmt.Errorf("managed PAC service set is empty")
	}

	admissionGeneration := m.closeReconciliationAdmission()
	m.opMu.Lock()
	defer m.opMu.Unlock()

	snapshot, err := m.Inspect(ctx)
	if err != nil {
		return InstallResult{}, err
	}
	installed, warnings := m.applyEligible(ctx, snapshot, selected, pacURL)
	state := RuntimeState{serviceNames: selected, pacURL: pacURL}
	result := InstallResult{state: state, installedServices: installed, warnings: warnings}
	if len(installed) == 0 {
		return result, errNoServicesInstalled
	}

	m.mu.Lock()
	if m.generation == admissionGeneration {
		m.accepting = true
	}
	m.mu.Unlock()
	return result, nil
}

// RequestReconcile records the latest desired PAC URL and returns immediately.
// Only the latest request is allowed to publish a completion.
func (m *ManagedPAC) RequestReconcile(state RuntimeState, pacURL string, complete func(ReconcileResult)) {
	m.mu.Lock()
	if !m.accepting {
		m.mu.Unlock()
		return
	}
	m.generation++
	if m.activeCancel != nil {
		m.activeCancel()
	}
	m.pending = &reconcileRequest{
		generation: m.generation,
		state:      cloneRuntimeState(state),
		pacURL:     pacURL,
		complete:   complete,
	}
	if m.worker {
		m.mu.Unlock()
		return
	}
	m.worker = true
	m.mu.Unlock()
	go m.runReconciliation()
}

func (m *ManagedPAC) runReconciliation() {
	for {
		m.mu.Lock()
		request := m.pending
		m.pending = nil
		if request == nil {
			m.worker = false
			m.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.activeCancel = cancel
		m.mu.Unlock()

		m.opMu.Lock()
		m.mu.Lock()
		current := m.accepting && request.generation == m.generation
		m.mu.Unlock()
		var result ReconcileResult
		if current {
			result = m.reconcile(ctx, request.state, request.pacURL)
		}
		m.opMu.Unlock()
		cancel()

		m.mu.Lock()
		if request.generation == m.generation {
			m.activeCancel = nil
		}
		latest := m.accepting && request.generation == m.generation
		m.mu.Unlock()
		if latest && request.complete != nil {
			request.complete(result)
		}
	}
}

func (m *ManagedPAC) reconcile(ctx context.Context, state RuntimeState, pacURL string) ReconcileResult {
	snapshot, err := m.Inspect(ctx)
	if err != nil {
		return ReconcileResult{err: err}
	}
	_, warnings := m.applyEligible(ctx, snapshot, state.serviceNames, pacURL)
	return ReconcileResult{warnings: warnings}
}

func (m *ManagedPAC) applyEligible(ctx context.Context, snapshot Snapshot, selected []string, pacURL string) ([]string, []Warning) {
	selectedSet := stringSet(selected)
	var installed []string
	var warnings []Warning
	for _, service := range snapshot.services {
		if _, ok := selectedSet[service.Name]; !ok {
			continue
		}
		if service.Ownership == OwnershipForeign {
			warnings = append(warnings, Warning{
				Kind:        WarningDrift,
				ServiceName: service.Name,
				Diagnostic:  "foreign PAC state is active",
			})
			continue
		}
		result, err := m.settings.Apply(ctx, pacURL, []string{service.Name})
		if err != nil {
			warnings = append(warnings, Warning{
				Kind:        WarningUpdateFailed,
				ServiceName: service.Name,
				Diagnostic:  err.Error(),
			})
			continue
		}
		if containsString(result.AppliedServices, service.Name) {
			installed = append(installed, service.Name)
		}
	}
	sort.Strings(installed)
	sortWarnings(warnings)
	return installed, warnings
}

// Uninstall closes reconciliation admission before waiting for the current
// writer, then removes all currently marker-owned settings and verifies absence.
func (m *ManagedPAC) Uninstall(ctx context.Context) error {
	m.closeReconciliationAdmission()
	m.opMu.Lock()
	defer m.opMu.Unlock()

	snapshot, err := m.Inspect(ctx)
	if err != nil {
		return err
	}
	var owned []string
	for _, service := range snapshot.services {
		if service.Ownership == OwnershipOwned {
			owned = append(owned, service.Name)
		}
	}
	clearErr := m.settings.ClearOwned(ctx, owned)
	after, inspectErr := m.Inspect(ctx)
	if inspectErr != nil {
		return errors.Join(clearErr, fmt.Errorf("managed PAC uninstall verification failed: %w", inspectErr))
	}
	var remaining []string
	for _, service := range after.services {
		if service.Ownership == OwnershipOwned {
			remaining = append(remaining, service.Name)
		}
	}
	if len(remaining) > 0 {
		return errors.Join(clearErr, fmt.Errorf("managed PAC state remains on services: %v", remaining))
	}
	return clearErr
}

func (m *ManagedPAC) closeReconciliationAdmission() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accepting = false
	m.generation++
	m.pending = nil
	if m.activeCancel != nil {
		m.activeCancel()
	}
	return m.generation
}

func cloneRuntimeState(state RuntimeState) RuntimeState {
	return RuntimeState{serviceNames: append([]string(nil), state.serviceNames...), pacURL: state.pacURL}
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func containsString(values []string, target string) bool {
	_, ok := stringSet(values)[target]
	return ok
}

func sortedUniqueStrings(values []string) []string {
	set := stringSet(values)
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortWarnings(warnings []Warning) {
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].ServiceName == warnings[j].ServiceName {
			return warnings[i].Kind < warnings[j].Kind
		}
		return warnings[i].ServiceName < warnings[j].ServiceName
	})
}

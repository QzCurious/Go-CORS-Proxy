package managedpac

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/conflatedstream"
)

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

// NewSnapshot returns an immutable semantic observation sorted by service name.
func NewSnapshot(services []Service) Snapshot {
	cloned := append([]Service(nil), services...)
	sort.Slice(cloned, func(i, j int) bool { return cloned[i].Name < cloned[j].Name })
	return Snapshot{services: cloned}
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

// NewRuntimeState returns immutable state for a fixed Managed PAC Service Set.
func NewRuntimeState(serviceNames []string, pacURL string) RuntimeState {
	return RuntimeState{
		serviceNames: sortedUniqueStrings(serviceNames),
		pacURL:       pacURL,
	}
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

// NewInstallResult returns an immutable Managed PAC installation result.
func NewInstallResult(state RuntimeState, installedServices []string, warnings []Warning) InstallResult {
	return InstallResult{
		state:             cloneRuntimeState(state),
		installedServices: append([]string(nil), installedServices...),
		warnings:          append([]Warning(nil), warnings...),
	}
}

func (r InstallResult) State() RuntimeState { return cloneRuntimeState(r.state) }

func (r InstallResult) InstalledServices() []string {
	return append([]string(nil), r.installedServices...)
}

func (r InstallResult) Warnings() []Warning {
	return append([]Warning(nil), r.warnings...)
}

// ManagedPAC owns PAC Projection publication generation, latest-value
// reconciliation, platform PAC mutation, and complete marker-based teardown.
type ManagedPAC struct {
	settings systemSettings

	opMu sync.Mutex
	mu   sync.Mutex

	accepting           bool
	admissionGeneration uint64
	activeCancel        context.CancelFunc

	projectionPublisher   conflatedstream.Publisher[string]
	projectionStream      conflatedstream.Stream[string]
	projectionWorkerDone  chan struct{}
	projectionWorkerStop  chan struct{}
	latestProjection      *string
	pacListen             string
	publicationGeneration uint64
	serviceNames          []string
}

const projectionPublicationRetry = 100 * time.Millisecond

func Open() *ManagedPAC {
	return openWithSettings(newSystemSettings())
}

func openWithSettings(settings systemSettings) *ManagedPAC {
	projectionPublisher, projectionStream := conflatedstream.New[string]()
	return &ManagedPAC{
		settings:            settings,
		projectionPublisher: projectionPublisher,
		projectionStream:    projectionStream,
	}
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
	return NewSnapshot(services), nil
}

func ownershipForURL(raw string) Ownership {
	if raw == "" || raw == "(null)" {
		return OwnershipEmpty
	}
	if IsOwnedURL(raw) {
		return OwnershipOwned
	}
	return OwnershipForeign
}

// InstallProjection performs the initial publication for a PAC Projection.
// Its generated URL is owned by Managed PAC's publication generation.
func (m *ManagedPAC) InstallProjection(ctx context.Context, serviceNames []string, pacListen, projection string) (InstallResult, error) {
	selected := sortedUniqueStrings(serviceNames)
	if len(selected) == 0 {
		return InstallResult{}, fmt.Errorf("managed PAC service set is empty")
	}

	admissionGeneration, workerDone := m.closeReconciliationAdmission()
	if workerDone != nil {
		<-workerDone
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	m.publicationGeneration++
	generation := m.publicationGeneration
	m.mu.Unlock()
	pacURL := PACURL(pacListen, generation)

	installed, warnings, publicationErr := m.attemptPublication(ctx, selected, pacURL)
	state := NewRuntimeState(selected, pacURL)
	result := NewInstallResult(state, installed, warnings)

	m.mu.Lock()
	m.latestProjection = &projection
	m.pacListen = pacListen
	m.serviceNames = append([]string(nil), selected...)
	if publicationErr != nil {
		m.projectionPublisher.Publish(projection)
	}
	if m.admissionGeneration == admissionGeneration {
		m.accepting = true
	}
	startWorker := m.accepting && m.projectionWorkerDone == nil
	var workerDoneToStart chan struct{}
	var workerStopToStart chan struct{}
	if startWorker {
		workerDoneToStart, workerStopToStart = m.startProjectionWorkerLocked()
	}
	m.mu.Unlock()
	if workerDoneToStart != nil {
		go m.runProjectionReconciliation(workerDoneToStart, workerStopToStart)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// Initial publication failures are retained inside Managed PAC and retried
	// by its projection worker. Gateway can continue serving its runtime.
	return result, nil
}

// PublishProjection records the newest changed PAC Projection and returns
// immediately. Managed PAC serializes and retries publication in its own worker.
func (m *ManagedPAC) PublishProjection(projection string) {
	m.mu.Lock()
	if !m.accepting {
		m.mu.Unlock()
		return
	}
	m.latestProjection = &projection
	m.projectionPublisher.Publish(projection)
	if m.projectionWorkerDone != nil {
		m.mu.Unlock()
		return
	}
	workerDone, workerStop := m.startProjectionWorkerLocked()
	m.mu.Unlock()
	go m.runProjectionReconciliation(workerDone, workerStop)
}

// PublicationGeneration returns the last generation allocated for a PAC
// publication attempt. Failed attempts intentionally consume generations.
func (m *ManagedPAC) PublicationGeneration() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.publicationGeneration
}

func (m *ManagedPAC) startProjectionWorkerLocked() (chan struct{}, chan struct{}) {
	done := make(chan struct{})
	stop := make(chan struct{})
	m.projectionWorkerDone = done
	m.projectionWorkerStop = stop
	return done, stop
}

func (m *ManagedPAC) runProjectionReconciliation(done chan struct{}, stop <-chan struct{}) {
	var retry *time.Timer
	var retryC <-chan time.Time
	defer func() {
		if retry != nil {
			retry.Stop()
		}
		m.mu.Lock()
		if m.projectionWorkerDone == done {
			m.projectionWorkerDone = nil
		}
		m.mu.Unlock()
		close(done)
	}()

	for {
		select {
		case <-stop:
			return
		case <-m.projectionStream.Updates():
			succeeded := m.reconcileLatestProjection()
			if succeeded {
				if retry != nil {
					retry.Stop()
				}
				retry = nil
				retryC = nil
			} else {
				retry, retryC = resetRetryTimer(retry)
			}
		case <-retryC:
			retry = nil
			retryC = nil
			if m.reconcileLatestProjection() {
				continue
			}
			retry, retryC = resetRetryTimer(retry)
		}

		m.mu.Lock()
		accepting := m.accepting
		m.mu.Unlock()
		if !accepting {
			return
		}
	}
}

func resetRetryTimer(previous *time.Timer) (*time.Timer, <-chan time.Time) {
	if previous != nil {
		previous.Stop()
	}
	timer := time.NewTimer(projectionPublicationRetry)
	return timer, timer.C
}

func (m *ManagedPAC) reconcileLatestProjection() bool {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	if !m.accepting || m.latestProjection == nil {
		m.mu.Unlock()
		return true
	}
	pacListen := m.pacListen
	serviceNames := append([]string(nil), m.serviceNames...)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	m.publicationGeneration++
	generation := m.publicationGeneration
	m.activeCancel = cancel
	m.mu.Unlock()
	pacURL := PACURL(pacListen, generation)
	_, _, err := m.attemptPublication(ctx, serviceNames, pacURL)
	cancel()

	m.mu.Lock()
	m.activeCancel = nil
	m.mu.Unlock()
	return err == nil
}

func (m *ManagedPAC) attemptPublication(ctx context.Context, serviceNames []string, pacURL string) ([]string, []Warning, error) {
	snapshot, err := m.Inspect(ctx)
	if err != nil {
		return nil, nil, err
	}
	installed, warnings := m.applyEligible(ctx, snapshot, serviceNames, pacURL)
	for _, warning := range warnings {
		if warning.Kind == WarningUpdateFailed {
			return installed, warnings, errors.New("managed PAC publication failed")
		}
	}
	return installed, warnings, nil
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
	_, workerDone := m.closeReconciliationAdmission()
	if workerDone != nil {
		<-workerDone
	}
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

func (m *ManagedPAC) closeReconciliationAdmission() (uint64, <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accepting = false
	m.admissionGeneration++
	if m.projectionWorkerStop != nil {
		close(m.projectionWorkerStop)
		m.projectionWorkerStop = nil
	}
	if m.activeCancel != nil {
		m.activeCancel()
	}
	return m.admissionGeneration, m.projectionWorkerDone
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

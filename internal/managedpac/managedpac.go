package managedpac

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/conflatedstream"
	"github.com/QzCurious/seamless-cors/internal/lib/pacsettings"
)

type Ownership string

const (
	OwnershipUnknown Ownership = "unknown"
	OwnershipEmpty   Ownership = "empty"
	OwnershipOwned   Ownership = "owned"
	OwnershipForeign Ownership = "foreign"
)

type Service struct {
	Name             string
	Enabled          bool
	URL              string
	Ownership        Ownership
	ObservationIssue *ObservationIssue
}

func (s Service) Manageable() bool {
	return s.ObservationIssue == nil && (s.Ownership == OwnershipEmpty || s.Ownership == OwnershipOwned)
}

type ObservationIssue struct {
	ServiceName string
	Diagnostic  string
}

type Snapshot struct {
	services []Service
}

// NewSnapshot returns a semantic observation sorted by service name.
func NewSnapshot(services []Service) Snapshot {
	cloned := append([]Service(nil), services...)
	sort.Slice(cloned, func(i, j int) bool { return cloned[i].Name < cloned[j].Name })
	return Snapshot{services: cloned}
}

func (s Snapshot) Services() []Service {
	return s.services
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

func (s Snapshot) ObservationIssues() []ObservationIssue {
	var issues []ObservationIssue
	for _, service := range s.services {
		if service.ObservationIssue != nil {
			issues = append(issues, *service.ObservationIssue)
		}
	}
	return issues
}

func (s Snapshot) HasOwnedState() bool {
	for _, service := range s.services {
		if service.Ownership == OwnershipOwned {
			return true
		}
	}
	return false
}

func (s Snapshot) HasActiveOwnedState() bool {
	for _, service := range s.services {
		if service.Enabled && service.Ownership == OwnershipOwned {
			return true
		}
	}
	return false
}

type RuntimeState struct {
	serviceNames []string
}

// NewRuntimeState returns read-only state for a fixed Managed PAC Service Set.
func NewRuntimeState(serviceNames []string) RuntimeState {
	return RuntimeState{serviceNames: sortedUniqueStrings(serviceNames)}
}

func (s RuntimeState) ServiceNames() []string {
	return s.serviceNames
}

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
	warnings          []Warning
	observationIssues []ObservationIssue
}

// ReconciliationResult is the latest complete Managed PAC runtime snapshot
// produced by one delivery attempt. Warnings replace the preceding
// snapshot; they are not an event history.
type ReconciliationResult struct {
	state             RuntimeState
	warnings          []Warning
	observationIssues []ObservationIssue
}

func NewReconciliationResult(state RuntimeState, warnings []Warning, observationIssues ...ObservationIssue) ReconciliationResult {
	return ReconciliationResult{state: state, warnings: warnings, observationIssues: observationIssues}
}

func (r ReconciliationResult) State() RuntimeState { return r.state }
func (r ReconciliationResult) Warnings() []Warning { return r.warnings }
func (r ReconciliationResult) ObservationIssues() []ObservationIssue {
	return r.observationIssues
}

// NewInstallResult returns a read-only Managed PAC installation result.
func NewInstallResult(state RuntimeState, warnings []Warning, observationIssues ...ObservationIssue) InstallResult {
	return InstallResult{state: state, warnings: warnings, observationIssues: observationIssues}
}

func (r InstallResult) State() RuntimeState { return r.state }

func (r InstallResult) Warnings() []Warning {
	return r.warnings
}

func (r InstallResult) ObservationIssues() []ObservationIssue {
	return r.observationIssues
}

type CleanupResult struct {
	observationIssues []ObservationIssue
}

func NewCleanupResult(observationIssues []ObservationIssue) CleanupResult {
	return CleanupResult{observationIssues: observationIssues}
}

func (r CleanupResult) ObservationIssues() []ObservationIssue {
	return r.observationIssues
}

// ManagedPAC owns per-service PAC URL delivery, serial mutation policy,
// retry, routing observation, and complete active-state teardown.
type ManagedPAC struct {
	settings pacsettings.Settings

	opMu sync.Mutex
	mu   sync.Mutex

	accepting           bool
	admissionGeneration uint64
	activeCancel        context.CancelFunc

	deliveryPublisher  conflatedstream.Publisher[struct{}]
	deliveryStream     conflatedstream.Stream[struct{}]
	resultPublisher    conflatedstream.Publisher[ReconciliationResult]
	resultStream       conflatedstream.Stream[ReconciliationResult]
	deliveryWorkerDone chan struct{}
	deliveryWorkerStop chan struct{}
	pacListen          string
	deliveryGeneration uint64
	serviceNames       []string
	runtimeState       RuntimeState
}

const deliveryRetry = 100 * time.Millisecond

func Open() *ManagedPAC {
	return openWithSettings(pacsettings.New())
}

func openWithSettings(settings pacsettings.Settings) *ManagedPAC {
	deliveryPublisher, deliveryStream := conflatedstream.New[struct{}]()
	resultPublisher, resultStream := conflatedstream.New[ReconciliationResult]()
	return &ManagedPAC{
		settings:          settings,
		deliveryPublisher: deliveryPublisher,
		deliveryStream:    deliveryStream,
		resultPublisher:   resultPublisher,
		resultStream:      resultStream,
	}
}

func (m *ManagedPAC) Inspect(ctx context.Context) (Snapshot, error) {
	serviceNames, err := m.settings.List(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("managed PAC inspection failed: %w", err)
	}
	services := make([]Service, 0, len(serviceNames))
	for _, serviceName := range serviceNames {
		setting, err := m.settings.Lookup(ctx, serviceName)
		if err != nil {
			if ctx.Err() != nil {
				return Snapshot{}, fmt.Errorf("managed PAC inspection canceled: %w", ctx.Err())
			}
			issue := ObservationIssue{ServiceName: serviceName, Diagnostic: err.Error()}
			services = append(services, Service{Name: serviceName, Ownership: OwnershipUnknown, ObservationIssue: &issue})
			continue
		}
		services = append(services, Service{
			Name:      serviceName,
			Enabled:   setting.Enabled,
			URL:       setting.URL,
			Ownership: ownershipForURL(setting.URL),
		})
	}
	return NewSnapshot(services), nil
}

// RoutingReady freshly observes whether any fixed managed service points to
// the PAC endpoint served by the current Gateway Runtime.
func (m *ManagedPAC) RoutingReady(ctx context.Context, pacListen string) (bool, []ObservationIssue, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	snapshot, err := m.Inspect(ctx)
	if err != nil {
		return false, nil, err
	}
	m.mu.Lock()
	selected := stringSet(m.serviceNames)
	m.mu.Unlock()
	for _, service := range snapshot.services {
		if _, ok := selected[service.Name]; !ok || service.ObservationIssue != nil {
			continue
		}
		if service.Enabled && service.Ownership == OwnershipOwned && servesRuntimePAC(service.URL, pacListen) {
			return true, snapshot.ObservationIssues(), nil
		}
	}
	return false, snapshot.ObservationIssues(), nil
}

// Install fixes the managed service set and delivers the currently served PAC.
func (m *ManagedPAC) Install(ctx context.Context, serviceNames []string, pacListen string) (InstallResult, error) {
	selected := sortedUniqueStrings(serviceNames)
	if len(selected) == 0 {
		return InstallResult{}, fmt.Errorf("managed PAC service set is empty")
	}

	admissionGeneration, workerDone := m.closeReconciliationAdmission()
	if workerDone != nil {
		<-workerDone
	}
	// A completed prior runtime must not leak a pending latest-value result
	// into the next Gateway Runtime activation.
	select {
	case <-m.resultStream.Updates():
	default:
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	m.deliveryGeneration++
	generation := m.deliveryGeneration
	m.mu.Unlock()
	pacURL := pacURL(pacListen, generation)

	warnings, observationIssues, deliveryFailed := m.attemptDelivery(ctx, selected, pacURL)
	state := NewRuntimeState(selected)
	result := NewInstallResult(state, warnings, observationIssues...)

	m.mu.Lock()
	m.pacListen = pacListen
	m.serviceNames = selected
	m.runtimeState = state
	if deliveryFailed {
		m.deliveryPublisher.Publish(struct{}{})
	}
	if m.admissionGeneration == admissionGeneration {
		m.accepting = true
	}
	startWorker := m.accepting && m.deliveryWorkerDone == nil
	var workerDoneToStart chan struct{}
	var workerStopToStart chan struct{}
	if startWorker {
		workerDoneToStart, workerStopToStart = m.startDeliveryWorkerLocked()
	}
	m.mu.Unlock()
	if workerDoneToStart != nil {
		go m.runDelivery(workerDoneToStart, workerStopToStart)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// Per-service delivery failures are warnings and retry work; Gateway keeps
	// serving the already-switched Traffic Projection.
	return result, nil
}

// ReconciliationResults delivers the latest per-service delivery outcome.
func (m *ManagedPAC) ReconciliationResults() <-chan ReconciliationResult {
	return m.resultStream.Updates()
}

// Deliver requests cache-busting delivery of the currently served PAC URL.
func (m *ManagedPAC) Deliver() {
	m.mu.Lock()
	if !m.accepting {
		m.mu.Unlock()
		return
	}
	m.deliveryPublisher.Publish(struct{}{})
	if m.deliveryWorkerDone != nil {
		m.mu.Unlock()
		return
	}
	workerDone, workerStop := m.startDeliveryWorkerLocked()
	m.mu.Unlock()
	go m.runDelivery(workerDone, workerStop)
}

// CleanupActiveState disables marker-owned PAC settings without changing
// reconciliation admission. Active lifecycles must use Uninstall instead.
func (m *ManagedPAC) CleanupActiveState(ctx context.Context) (CleanupResult, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	accepting := m.accepting
	m.mu.Unlock()
	if accepting {
		return CleanupResult{}, CleanupWhileReconciliationActiveError{}
	}
	return m.cleanupActiveStateLocked(ctx)
}

// Uninstall closes reconciliation admission before waiting for the current
// writer, then disables all currently active marker-owned settings.
func (m *ManagedPAC) Uninstall(ctx context.Context) (CleanupResult, error) {
	_, workerDone := m.closeReconciliationAdmission()
	if workerDone != nil {
		<-workerDone
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	result, err := m.cleanupActiveStateLocked(ctx)
	if err == nil {
		m.mu.Lock()
		m.runtimeState = RuntimeState{}
		m.serviceNames = nil
		m.pacListen = ""
		m.mu.Unlock()
	}
	return result, err
}

func (m *ManagedPAC) startDeliveryWorkerLocked() (chan struct{}, chan struct{}) {
	done := make(chan struct{})
	stop := make(chan struct{})
	m.deliveryWorkerDone = done
	m.deliveryWorkerStop = stop
	return done, stop
}

func (m *ManagedPAC) runDelivery(done chan struct{}, stop <-chan struct{}) {
	var retry *time.Timer
	var retryC <-chan time.Time
	defer func() {
		if retry != nil {
			retry.Stop()
		}
		m.mu.Lock()
		if m.deliveryWorkerDone == done {
			m.deliveryWorkerDone = nil
		}
		m.mu.Unlock()
		close(done)
	}()

	for {
		select {
		case <-stop:
			return
		case <-m.deliveryStream.Updates():
			succeeded := m.reconcileDelivery()
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
			if m.reconcileDelivery() {
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
	timer := time.NewTimer(deliveryRetry)
	return timer, timer.C
}

func (m *ManagedPAC) reconcileDelivery() bool {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	if !m.accepting || m.pacListen == "" {
		m.mu.Unlock()
		return true
	}
	pacListen := m.pacListen
	serviceNames := m.serviceNames

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	m.deliveryGeneration++
	generation := m.deliveryGeneration
	m.activeCancel = cancel
	m.mu.Unlock()
	pacURL := pacURL(pacListen, generation)
	warnings, observationIssues, failed := m.attemptDelivery(ctx, serviceNames, pacURL)
	cancel()

	m.mu.Lock()
	m.activeCancel = nil
	state := m.runtimeState
	m.mu.Unlock()
	m.resultPublisher.Publish(NewReconciliationResult(state, warnings, observationIssues...))
	return !failed
}

func (m *ManagedPAC) attemptDelivery(ctx context.Context, serviceNames []string, pacURL string) ([]Warning, []ObservationIssue, bool) {
	snapshot, err := m.Inspect(ctx)
	if err != nil {
		return []Warning{{Kind: WarningUpdateFailed, Diagnostic: err.Error()}}, nil, true
	}
	warnings, observationIssues := m.applyEligible(ctx, snapshot, serviceNames, pacURL)
	for _, warning := range warnings {
		if warning.Kind == WarningUpdateFailed {
			return warnings, observationIssues, true
		}
	}
	return warnings, observationIssues, false
}

func (m *ManagedPAC) applyEligible(ctx context.Context, snapshot Snapshot, selected []string, pacURL string) ([]Warning, []ObservationIssue) {
	selectedSet := stringSet(selected)
	var warnings []Warning
	var observationIssues []ObservationIssue
	for _, service := range snapshot.services {
		if _, ok := selectedSet[service.Name]; !ok {
			continue
		}
		if service.ObservationIssue != nil {
			observationIssues = append(observationIssues, *service.ObservationIssue)
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
		current, err := m.settings.Lookup(ctx, service.Name)
		if err != nil {
			if ctx.Err() != nil {
				warnings = append(warnings, Warning{Kind: WarningUpdateFailed, ServiceName: service.Name, Diagnostic: err.Error()})
				continue
			}
			observationIssues = append(observationIssues, ObservationIssue{ServiceName: service.Name, Diagnostic: err.Error()})
			continue
		}
		if ownershipForURL(current.URL) == OwnershipForeign {
			warnings = append(warnings, Warning{
				Kind:        WarningDrift,
				ServiceName: service.Name,
				Diagnostic:  "foreign PAC state is active",
			})
			continue
		}
		if err := m.settings.SetURL(ctx, service.Name, pacURL); err != nil {
			warnings = append(warnings, Warning{
				Kind:        WarningUpdateFailed,
				ServiceName: service.Name,
				Diagnostic:  err.Error(),
			})
		}
	}
	sortWarnings(warnings)
	return warnings, sortedObservationIssues(observationIssues)
}

func (m *ManagedPAC) cleanupActiveStateLocked(ctx context.Context) (CleanupResult, error) {
	snapshot, err := m.Inspect(ctx)
	if err != nil {
		return CleanupResult{}, ActiveStateCleanupError{InspectionErr: err}
	}
	observationIssues := snapshot.ObservationIssues()
	var activeOwned []Service
	for _, service := range snapshot.services {
		if service.Enabled && service.Ownership == OwnershipOwned {
			activeOwned = append(activeOwned, service)
		}
	}
	serviceFailures := make([]ServiceCleanupFailure, 0)
	for _, service := range activeOwned {
		current, err := m.settings.Lookup(ctx, service.Name)
		if err != nil {
			if ctx.Err() != nil {
				serviceFailures = append(serviceFailures, ServiceCleanupFailure{ServiceName: service.Name, Err: err})
				continue
			}
			observationIssues = append(observationIssues, ObservationIssue{ServiceName: service.Name, Diagnostic: err.Error()})
			continue
		}
		if !current.Enabled || ownershipForURL(current.URL) != OwnershipOwned {
			continue
		}
		if err := m.settings.Disable(ctx, service.Name); err != nil {
			serviceFailures = append(serviceFailures, ServiceCleanupFailure{ServiceName: service.Name, Err: err})
		}
	}
	after, inspectErr := m.Inspect(ctx)
	if inspectErr != nil {
		return NewCleanupResult(sortedObservationIssues(observationIssues)), ActiveStateCleanupError{ServiceFailures: serviceFailures, VerificationErr: inspectErr}
	}
	observationIssues = append(observationIssues, after.ObservationIssues()...)
	var remaining []string
	for _, service := range after.services {
		if service.Enabled && service.Ownership == OwnershipOwned {
			remaining = append(remaining, service.Name)
		}
	}
	remainingSet := stringSet(remaining)
	serviceFailures = slices.DeleteFunc(serviceFailures, func(failure ServiceCleanupFailure) bool {
		_, remains := remainingSet[failure.ServiceName]
		return !remains
	})
	if len(serviceFailures) > 0 || len(remaining) > 0 {
		return NewCleanupResult(sortedObservationIssues(observationIssues)), ActiveStateCleanupError{ServiceFailures: serviceFailures, RemainingServices: remaining}
	}
	return NewCleanupResult(sortedObservationIssues(observationIssues)), nil
}

type CleanupWhileReconciliationActiveError struct{}

func (CleanupWhileReconciliationActiveError) Error() string {
	return "managed PAC active-state cleanup rejected while reconciliation is active"
}

type ServiceCleanupFailure struct {
	ServiceName string
	Err         error
}

func (e ServiceCleanupFailure) Error() string {
	return fmt.Sprintf("%s: %v", e.ServiceName, e.Err)
}

func (e ServiceCleanupFailure) Unwrap() error { return e.Err }

type ActiveStateCleanupError struct {
	InspectionErr     error
	ServiceFailures   []ServiceCleanupFailure
	VerificationErr   error
	RemainingServices []string
}

func (e ActiveStateCleanupError) Error() string {
	parts := make([]string, 0, 3)
	if e.InspectionErr != nil {
		parts = append(parts, "inspection failed: "+e.InspectionErr.Error())
	}
	if len(e.ServiceFailures) > 0 {
		failures := make([]string, 0, len(e.ServiceFailures))
		for _, failure := range e.ServiceFailures {
			failures = append(failures, failure.Error())
		}
		parts = append(parts, "service mutations failed: "+strings.Join(failures, "; "))
	}
	if e.VerificationErr != nil {
		parts = append(parts, "verification failed: "+e.VerificationErr.Error())
	} else if len(e.RemainingServices) > 0 {
		parts = append(parts, fmt.Sprintf("active state remains on services: %v", e.RemainingServices))
	}
	return "managed PAC active-state cleanup failed: " + strings.Join(parts, "; ")
}

func (e ActiveStateCleanupError) Unwrap() []error {
	causes := make([]error, 0, len(e.ServiceFailures)+2)
	if e.InspectionErr != nil {
		causes = append(causes, e.InspectionErr)
	}
	for _, failure := range e.ServiceFailures {
		causes = append(causes, failure)
	}
	if e.VerificationErr != nil {
		causes = append(causes, e.VerificationErr)
	}
	return causes
}

func ownershipForURL(raw string) Ownership {
	if raw == "" {
		return OwnershipEmpty
	}
	if isOwnedURL(raw) {
		return OwnershipOwned
	}
	return OwnershipForeign
}

func servesRuntimePAC(raw, pacListen string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host != pacListen {
		return false
	}
	return parsed.Path == "/seamless-cors.pac"
}

func (m *ManagedPAC) closeReconciliationAdmission() (uint64, <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accepting = false
	m.admissionGeneration++
	if m.deliveryWorkerStop != nil {
		close(m.deliveryWorkerStop)
		m.deliveryWorkerStop = nil
	}
	if m.activeCancel != nil {
		m.activeCancel()
	}
	return m.admissionGeneration, m.deliveryWorkerDone
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func sortedObservationIssues(issues []ObservationIssue) []ObservationIssue {
	byService := make(map[string]ObservationIssue, len(issues))
	for _, issue := range issues {
		byService[issue.ServiceName] = issue
	}
	out := make([]ObservationIssue, 0, len(byService))
	for _, issue := range byService {
		out = append(out, issue)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceName < out[j].ServiceName })
	return out
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

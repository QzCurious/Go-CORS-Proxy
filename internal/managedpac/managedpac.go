package managedpac

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/networkservice"
)

// Activation is the Gateway Activation-facing Managed PAC capability.
type Activation interface {
	Begin(context.Context) (Control, Assessment, error)
}

// Footprint is the ownerless inspection and cleanup Managed PAC capability.
type Footprint interface {
	InspectFootprint(context.Context) (FootprintReport, error)
	Cleanup(context.Context) (CleanupReport, error)
}

// Control is one activation-scoped Managed PAC control lifetime.
type Control interface {
	Deliver(string) (ControlState, error)
	Observe() (ControlState, error)
	Close() (CleanupReport, error)
}

// ManagedPAC owns inspection and cleanup of seamless-cors PAC settings and
// creates fixed-set control lifetimes for Gateway Runtime activations.
type ManagedPAC struct {
	listServices func(context.Context) ([]networkservice.Service, error)
}

func New() *ManagedPAC {
	return &ManagedPAC{listServices: networkservice.List}
}

var (
	_ Activation = (*ManagedPAC)(nil)
	_ Footprint  = (*ManagedPAC)(nil)
)

// Assessment is Managed PAC's activation-specific view of the visible Network
// Services and the fixed Managed PAC Service Set selected from them.
type Assessment struct {
	Services          []AssessedService
	ServiceSet        []string
	ObservationIssues []ObservationIssue
}

func (a Assessment) HasManageableServices() bool {
	return len(a.ServiceSet) > 0
}

// AssessedService is a presentation fact from one activation assessment.
// Manageable is authoritative; callers do not derive it from the other fields.
type AssessedService struct {
	ServiceName string    `json:"serviceName"`
	URL         string    `json:"url"`
	Enabled     bool      `json:"enabled"`
	Ownership   Ownership `json:"ownership"`
	Manageable  bool      `json:"manageable"`
}

// ControlState is the normalized live view of one Managed PAC control
// lifetime. RoutesCurrentEndpoint is a Managed PAC fact, not Gateway's
// cross-feature Traffic Routing Ready fact.
type ControlState struct {
	ServiceSet            []string
	RoutesCurrentEndpoint bool
	Warnings              []Warning
	ObservationIssues     []ObservationIssue
}

type FootprintState string

const (
	FootprintNone          FootprintState = "none"
	FootprintCleanupNeeded FootprintState = "cleanup-needed"
)

// FootprintReport is Managed PAC's ownerless active-state conclusion.
type FootprintReport struct {
	State FootprintState
}

// CleanupReport contains non-blocking observation issues found while cleanup
// otherwise established its postcondition.
type CleanupReport struct {
	ObservationIssues []ObservationIssue
}

type Ownership string

const (
	OwnershipUnknown Ownership = "unknown"
	OwnershipEmpty   Ownership = "empty"
	OwnershipOwned   Ownership = "owned"
	OwnershipForeign Ownership = "foreign"
)

type WarningKind string

const (
	WarningDrift        WarningKind = "drift"
	WarningUpdateFailed WarningKind = "update-failed"
)

// Warning is one service-identified current Managed PAC diagnostic.
type Warning struct {
	Kind        WarningKind `json:"kind"`
	ServiceName string      `json:"serviceName,omitempty"`
	Diagnostic  string      `json:"diagnostic"`
}

// ObservationIssue is one service-identified PAC setting observation failure.
type ObservationIssue struct {
	ServiceName string `json:"serviceName"`
	Diagnostic  string `json:"diagnostic"`
}

// Begin fixes the currently manageable Network Services for one control
// lifetime without mutating PAC settings and returns an activation assessment.
func (m *ManagedPAC) Begin(ctx context.Context) (Control, Assessment, error) {
	observed, err := m.inspect(ctx)
	if err != nil {
		return nil, Assessment{}, err
	}
	// The caller context governs creation. Once created, the control lifetime is
	// ended only by Close, not by the request that happened to create it.
	controlCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	return &control{
		owner:        m,
		ctx:          controlCtx,
		cancel:       cancel,
		serviceNames: observed.manageableServices(),
	}, assessmentFrom(observed), nil
}

// InspectFootprint reports whether freshly observed active marker-owned PAC
// state requires ownerless cleanup.
func (m *ManagedPAC) InspectFootprint(ctx context.Context) (FootprintReport, error) {
	observed, err := m.inspect(ctx)
	if err != nil {
		return FootprintReport{}, err
	}
	state := FootprintNone
	if observed.hasActiveOwnedState() {
		state = FootprintCleanupNeeded
	}
	return FootprintReport{State: state}, nil
}

// Cleanup disables and verifies every freshly observed active marker-owned PAC
// setting. Gateway uses it only when no live control handle exists.
func (m *ManagedPAC) Cleanup(ctx context.Context) (CleanupReport, error) {
	issues, err := m.cleanup(ctx)
	return CleanupReport{ObservationIssues: issues}, err
}

// control owns one fixed Managed PAC Service Set for its lifetime.
type control struct {
	owner  *ManagedPAC
	ctx    context.Context
	cancel context.CancelFunc

	// mu lets Close reject and cancel work before waiting for opMu. All other
	// mutable control state is serialized by opMu.
	mu     sync.Mutex
	closed bool
	opMu   sync.Mutex

	pacListen          string
	deliveryGeneration uint64
	serviceNames       []string
	deliveryWarnings   map[string][]Warning

	closeOnce   sync.Once
	closeIssues []ObservationIssue
	closeErr    error
}

var _ Control = (*control)(nil)

// Deliver performs one serialized attempt to deliver pacListen to every currently
// eligible member of the fixed service set.
func (c *control) Deliver(pacListen string) (ControlState, error) {
	if pacListen == "" {
		return ControlState{}, fmt.Errorf("managed PAC endpoint is empty")
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ControlState{}, controlClosedError{}
	}
	ctx := c.ctx
	c.mu.Unlock()

	c.deliveryGeneration++
	c.pacListen = pacListen
	nextURL := pacURL(pacListen, c.deliveryGeneration)
	state := controlSnapshot(c.serviceNames, nil)
	defer func() {
		c.deliveryWarnings = warningsByService(state)
	}()

	networkServices, err := c.owner.listServices(ctx)
	if err != nil {
		return controlStateFrom(state), fmt.Errorf("managed PAC inspection failed: %w", err)
	}
	visible := networkServicesByName(networkServices)

	// Keep observation and mutation in separate phases so one service's update
	// cannot influence the fresh classification of a later service.
	for index := range state.services {
		service := &state.services[index]
		networkService, ok := visible[service.ServiceName]
		if !ok {
			continue
		}
		setting, lookupErr := networkService.PAC(ctx)
		if lookupErr != nil {
			if ctx.Err() != nil {
				service.Warnings = []Warning{{Kind: WarningUpdateFailed, ServiceName: service.ServiceName, Diagnostic: lookupErr.Error()}}
				return controlStateFrom(state), ctx.Err()
			}
			service.ObservationIssue = lookupErr.Error()
			continue
		}
		*service = observedService(networkService.Name(), setting)
		service.Controlled = service.Enabled && service.Ownership == OwnershipOwned && servesRuntimePAC(service.URL, pacListen)
		if service.Ownership == OwnershipForeign {
			service.Warnings = []Warning{{Kind: WarningDrift, ServiceName: service.ServiceName, Diagnostic: "foreign PAC state is active"}}
		}
	}

	for index := range state.services {
		service := &state.services[index]
		if !service.manageable() {
			continue
		}
		networkService := visible[service.ServiceName]
		if setErr := networkService.SetPAC(ctx, nextURL); setErr != nil {
			service.Warnings = []Warning{{Kind: WarningUpdateFailed, ServiceName: service.ServiceName, Diagnostic: setErr.Error()}}
			if ctx.Err() != nil {
				return controlStateFrom(state), ctx.Err()
			}
			continue
		}
		service.URL = nextURL
		service.Enabled = true
		service.Ownership = OwnershipOwned
		service.Controlled = true
	}

	return controlStateFrom(state), nil
}

// Observe freshly observes the fixed service set without mutating PAC settings.
func (c *control) Observe() (ControlState, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ControlState{}, controlClosedError{}
	}
	ctx := c.ctx
	c.mu.Unlock()
	observed, err := c.observe(ctx)
	return controlStateFrom(observed), err
}

// Close rejects new work, cancels and waits for in-flight work, then performs
// bounded complete cleanup of freshly observed active marker-owned settings.
func (c *control) Close() (CleanupReport, error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.cancel()
		c.mu.Unlock()

		c.opMu.Lock()
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), cleanupTimeout)
		c.closeIssues, c.closeErr = c.owner.cleanup(cleanupCtx)
		cancel()
		c.opMu.Unlock()
	})
	return CleanupReport{ObservationIssues: c.closeIssues}, c.closeErr
}

// serviceState is Managed PAC's private current state for one Network Service.
type serviceState struct {
	ServiceName      string
	URL              string
	Enabled          bool
	Ownership        Ownership
	Controlled       bool
	Warnings         []Warning
	ObservationIssue string
}

func (s serviceState) manageable() bool {
	return s.ObservationIssue == "" && (s.Ownership == OwnershipEmpty || s.Ownership == OwnershipOwned)
}

type snapshot struct {
	services []serviceState
}

func (s snapshot) manageableServices() []string {
	names := make([]string, 0, len(s.services))
	for _, service := range s.services {
		if service.manageable() {
			names = append(names, service.ServiceName)
		}
	}
	return names
}

func (s snapshot) hasActiveOwnedState() bool {
	for _, service := range s.services {
		if service.Enabled && service.Ownership == OwnershipOwned {
			return true
		}
	}
	return false
}

func (m *ManagedPAC) inspect(ctx context.Context) (snapshot, error) {
	networkServices, err := m.listServices(ctx)
	if err != nil {
		return snapshot{}, fmt.Errorf("managed PAC inspection failed: %w", err)
	}
	services := make([]serviceState, 0, len(networkServices))
	for _, networkService := range networkServices {
		setting, err := networkService.PAC(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return snapshot{}, fmt.Errorf("managed PAC inspection canceled: %w", ctx.Err())
			}
			services = append(services, serviceState{
				ServiceName:      networkService.Name(),
				Ownership:        OwnershipUnknown,
				ObservationIssue: err.Error(),
			})
			continue
		}
		services = append(services, observedService(networkService.Name(), setting))
	}
	return snapshot{services: services}, nil
}

func (c *control) observe(ctx context.Context) (snapshot, error) {
	state := controlSnapshot(c.serviceNames, c.deliveryWarnings)
	networkServices, err := c.owner.listServices(ctx)
	if err != nil {
		return state, fmt.Errorf("managed PAC inspection failed: %w", err)
	}
	visible := networkServicesByName(networkServices)
	for index := range state.services {
		service := &state.services[index]
		networkService, ok := visible[service.ServiceName]
		if !ok {
			continue
		}
		setting, lookupErr := networkService.PAC(ctx)
		if lookupErr != nil {
			if ctx.Err() != nil {
				return state, fmt.Errorf("managed PAC inspection canceled: %w", ctx.Err())
			}
			service.ObservationIssue = lookupErr.Error()
			continue
		}
		warnings := service.Warnings
		*service = observedService(networkService.Name(), setting)
		service.Warnings = warnings
		service.Controlled = service.Enabled && service.Ownership == OwnershipOwned && servesRuntimePAC(service.URL, c.pacListen)
		if service.Ownership == OwnershipForeign && !hasWarningKind(service.Warnings, WarningDrift) {
			service.Warnings = append(service.Warnings, Warning{
				Kind: WarningDrift, ServiceName: service.ServiceName, Diagnostic: "foreign PAC state is active",
			})
		}
	}
	return state, nil
}

func observedService(serviceName string, setting networkservice.PACSetting) serviceState {
	return serviceState{
		ServiceName: serviceName,
		URL:         setting.URL,
		Enabled:     setting.Enabled,
		Ownership:   ownershipForURL(setting.URL),
	}
}

func controlSnapshot(serviceNames []string, retained map[string][]Warning) snapshot {
	services := make([]serviceState, len(serviceNames))
	for index, serviceName := range serviceNames {
		services[index] = serviceState{
			ServiceName: serviceName,
			Ownership:   OwnershipUnknown,
			Warnings:    append([]Warning(nil), retained[serviceName]...),
		}
	}
	return snapshot{services: services}
}

func warningsByService(snapshot snapshot) map[string][]Warning {
	warnings := make(map[string][]Warning)
	for _, service := range snapshot.services {
		if len(service.Warnings) > 0 {
			warnings[service.ServiceName] = service.Warnings
		}
	}
	return warnings
}

func assessmentFrom(observed snapshot) Assessment {
	assessment := Assessment{
		Services:   make([]AssessedService, 0, len(observed.services)),
		ServiceSet: observed.manageableServices(),
	}
	for _, service := range observed.services {
		assessment.Services = append(assessment.Services, AssessedService{
			ServiceName: service.ServiceName,
			URL:         service.URL,
			Enabled:     service.Enabled,
			Ownership:   service.Ownership,
			Manageable:  service.manageable(),
		})
		if service.ObservationIssue != "" {
			assessment.ObservationIssues = append(assessment.ObservationIssues, ObservationIssue{
				ServiceName: service.ServiceName,
				Diagnostic:  service.ObservationIssue,
			})
		}
	}
	return assessment
}

func controlStateFrom(observed snapshot) ControlState {
	state := ControlState{ServiceSet: make([]string, 0, len(observed.services))}
	for _, service := range observed.services {
		state.ServiceSet = append(state.ServiceSet, service.ServiceName)
		state.RoutesCurrentEndpoint = state.RoutesCurrentEndpoint || service.Controlled
		state.Warnings = append(state.Warnings, service.Warnings...)
		if service.ObservationIssue != "" {
			state.ObservationIssues = append(state.ObservationIssues, ObservationIssue{
				ServiceName: service.ServiceName,
				Diagnostic:  service.ObservationIssue,
			})
		}
	}
	return state
}

func hasWarningKind(warnings []Warning, kind WarningKind) bool {
	for _, warning := range warnings {
		if warning.Kind == kind {
			return true
		}
	}
	return false
}

const cleanupTimeout = 5 * time.Second

func (m *ManagedPAC) cleanup(ctx context.Context) ([]ObservationIssue, error) {
	networkServices, err := m.listServices(ctx)
	if err != nil {
		return nil, activeStateCleanupError{
			inspectionErr: fmt.Errorf("managed PAC inspection failed: %w", err),
		}
	}
	var observationIssues []ObservationIssue
	var serviceFailures []serviceCleanupFailure
	for _, networkService := range networkServices {
		serviceName := networkService.Name()
		setting, lookupErr := networkService.PAC(ctx)
		if lookupErr != nil {
			if ctx.Err() != nil {
				serviceFailures = append(serviceFailures, serviceCleanupFailure{serviceName: serviceName, err: lookupErr})
				continue
			}
			observationIssues = append(observationIssues, ObservationIssue{ServiceName: serviceName, Diagnostic: lookupErr.Error()})
			continue
		}
		if !setting.Enabled || ownershipForURL(setting.URL) != OwnershipOwned {
			continue
		}
		if disableErr := networkService.DisablePAC(ctx); disableErr != nil {
			serviceFailures = append(serviceFailures, serviceCleanupFailure{serviceName: serviceName, err: disableErr})
		}
	}

	after, inspectErr := m.inspect(ctx)
	if inspectErr != nil {
		return uniqueObservationIssues(observationIssues), activeStateCleanupError{
			serviceFailures: serviceFailures,
			verificationErr: inspectErr,
		}
	}
	var remaining []string
	for _, service := range after.services {
		if service.ObservationIssue != "" {
			observationIssues = append(observationIssues, ObservationIssue{
				ServiceName: service.ServiceName,
				Diagnostic:  service.ObservationIssue,
			})
		}
		if service.Enabled && service.Ownership == OwnershipOwned {
			remaining = append(remaining, service.ServiceName)
		}
	}
	remainingSet := stringSet(remaining)
	retainedFailures := serviceFailures[:0]
	for _, failure := range serviceFailures {
		if _, remains := remainingSet[failure.serviceName]; remains {
			retainedFailures = append(retainedFailures, failure)
		}
	}
	serviceFailures = retainedFailures
	observationIssues = uniqueObservationIssues(observationIssues)
	if len(serviceFailures) > 0 || len(remaining) > 0 {
		return observationIssues, activeStateCleanupError{
			serviceFailures:   serviceFailures,
			remainingServices: remaining,
		}
	}
	return observationIssues, nil
}

func networkServicesByName(services []networkservice.Service) map[string]networkservice.Service {
	indexed := make(map[string]networkservice.Service, len(services))
	for _, service := range services {
		indexed[service.Name()] = service
	}
	return indexed
}

type controlClosedError struct{}

func (controlClosedError) Error() string {
	return "managed PAC control is closed"
}

type serviceCleanupFailure struct {
	serviceName string
	err         error
}

func (e serviceCleanupFailure) Error() string {
	return fmt.Sprintf("%s: %v", e.serviceName, e.err)
}

func (e serviceCleanupFailure) Unwrap() error { return e.err }

type activeStateCleanupError struct {
	inspectionErr     error
	serviceFailures   []serviceCleanupFailure
	verificationErr   error
	remainingServices []string
}

func (e activeStateCleanupError) Error() string {
	parts := make([]string, 0, 3)
	if e.inspectionErr != nil {
		parts = append(parts, "inspection failed: "+e.inspectionErr.Error())
	}
	if len(e.serviceFailures) > 0 {
		failures := make([]string, 0, len(e.serviceFailures))
		for _, failure := range e.serviceFailures {
			failures = append(failures, failure.Error())
		}
		parts = append(parts, "service mutations failed: "+strings.Join(failures, "; "))
	}
	if e.verificationErr != nil {
		parts = append(parts, "verification failed: "+e.verificationErr.Error())
	} else if len(e.remainingServices) > 0 {
		parts = append(parts, fmt.Sprintf("active state remains on services: %v", e.remainingServices))
	}
	return "managed PAC active-state cleanup failed: " + strings.Join(parts, "; ")
}

func (e activeStateCleanupError) Unwrap() []error {
	causes := make([]error, 0, len(e.serviceFailures)+2)
	if e.inspectionErr != nil {
		causes = append(causes, e.inspectionErr)
	}
	for _, failure := range e.serviceFailures {
		causes = append(causes, failure)
	}
	if e.verificationErr != nil {
		causes = append(causes, e.verificationErr)
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

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func uniqueObservationIssues(issues []ObservationIssue) []ObservationIssue {
	indexes := make(map[string]int, len(issues))
	out := make([]ObservationIssue, 0, len(issues))
	for _, issue := range issues {
		if index, ok := indexes[issue.ServiceName]; ok {
			out[index] = issue
			continue
		}
		indexes[issue.ServiceName] = len(out)
		out = append(out, issue)
	}
	return out
}

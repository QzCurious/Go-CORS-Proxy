package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

var (
	errCAOperationInProgress = errors.New("ca-operation-in-progress")
	errGatewayOwnerEnding    = errors.New("gateway owner is ending")
	errOwnerTransition       = errors.New("gateway ownership is transitioning")
)

type StartResultKind string

const (
	StartResultStarted                StartResultKind = "started"
	StartResultAlreadyRunning         StartResultKind = "already-running"
	StartResultOwnerAlreadyRunning    StartResultKind = "owner-already-running"
	StartResultOwnerTransition        StartResultKind = "owner-transition"
	StartResultConsentRequired        StartResultKind = "consent-required"
	StartResultPACReplacementDeclined StartResultKind = "pac-replacement-declined"
	StartResultStartAlreadyMutating   StartResultKind = "start-already-mutating"
	StartResultStopCancelled          StartResultKind = "stop-cancelled"
	StartResultCleanupFailed          StartResultKind = "cleanup-failed"
)

type StartError struct {
	Diagnostic string `json:"diagnostic"`
	Cause      error  `json:"-"`
}

func (e *StartError) Error() string { return e.Diagnostic }
func (e *StartError) Unwrap() error { return e.Cause }

type StartRequest struct {
	PACReplacementConsent *PACReplacementConsentInput `json:"pacReplacementConsent,omitempty"`
}

type StartResult struct {
	Kind                  StartResultKind              `json:"kind"`
	PACReplacementConsent *PACReplacementConsentDetail `json:"pacReplacementConsent,omitempty"`
	Guidance              *StartGuidanceDetail         `json:"guidance,omitempty"`
	CleanupFailures       []CleanupFailureDetail       `json:"cleanupFailures,omitempty"`
}

type PACReplacementConsentDetail struct {
	CurrentPACState []ManagedPACServiceState `json:"currentPacState"`
	CleanupMode     CleanupMode              `json:"cleanupMode"`
	Fingerprint     PACConsentFingerprint    `json:"fingerprint"`
}

type CleanupMode string

const CleanupModeNoPACRestoration CleanupMode = "no-pac-restoration"

type ManagedPACServiceState struct {
	ServiceName                string       `json:"serviceName"`
	Enabled                    bool         `json:"enabled"`
	URL                        string       `json:"url"`
	Ownership                  PACOwnership `json:"ownership"`
	ReplacementConsentRequired bool         `json:"replacementConsentRequired"`
}

type PACOwnership string

const (
	PACOwnershipEmpty   PACOwnership = "empty"
	PACOwnershipOwned   PACOwnership = "owned"
	PACOwnershipForeign PACOwnership = "foreign"
)

type PACReplacementConsentInput struct {
	Accepted    bool                  `json:"accepted"`
	Fingerprint PACConsentFingerprint `json:"fingerprint"`
}

type PACConsentFingerprint string

type StartGuidanceDetail struct {
	UpstreamListPath     string                      `json:"upstreamListPath"`
	ManagedPACActive     bool                        `json:"managedPacActive"`
	ManagedPACServices   []string                    `json:"managedPacServices,omitempty"`
	HTTPSReadiness       HTTPSReadinessStatus        `json:"httpsReadiness"`
	HTTPSInterception    HTTPSInterceptionState      `json:"httpsInterception"`
	HTTPSIntent          bool                        `json:"httpsIntent"`
	HTTPSWarnings        []HTTPSWarningDetail        `json:"httpsWarnings,omitempty"`
	UpstreamListWarnings []UpstreamListWarningDetail `json:"upstreamListWarnings,omitempty"`
}

type UpstreamListWarningDetail struct {
	Line       int    `json:"line"`
	Text       string `json:"text"`
	Diagnostic string `json:"diagnostic"`
}

type StopResultKind string

const (
	StopResultStopped                 StopResultKind = "stopped"
	StopResultNotRunning              StopResultKind = "not-running"
	StopResultCleanupFailed           StopResultKind = "cleanup-failed"
	StopResultNotRunningCleanupFailed StopResultKind = "not-running-cleanup-failed"
)

type StopResult struct {
	Kind            StopResultKind         `json:"kind"`
	Warnings        []CommandWarning       `json:"warnings,omitempty"`
	CleanupFailures []CleanupFailureDetail `json:"cleanupFailures,omitempty"`
}

type CleanupFailureDetail struct {
	Subject    CleanupSubjectKind `json:"subject"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

type CleanupSubjectKind string

const (
	CleanupSubjectManagedPAC        CleanupSubjectKind = "managed-pac"
	CleanupSubjectGatewayStateCache CleanupSubjectKind = "gateway-state-cache"
)

type CommandWarning struct {
	Kind       CommandWarningKind `json:"kind"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

type CommandWarningKind string

const (
	CommandWarningRuntimeCloseFailed CommandWarningKind = "runtime-close-failed"
)

type InstallResultKind string

const (
	InstallResultInstalled             InstallResultKind = "installed"
	InstallResultAlreadyUsable         InstallResultKind = "already-usable"
	InstallResultRuntimeAdoptionFailed InstallResultKind = "installed-runtime-adoption-failed"
	InstallResultPACRefreshFailed      InstallResultKind = "installed-pac-refresh-failed"
)

type InstallResult struct {
	Kind               InstallResultKind    `json:"kind"`
	InstalledCAExpires time.Time            `json:"installedCAExpires,omitempty"`
	Warnings           []HTTPSWarningDetail `json:"warnings,omitempty"`
}

type UninstallResultKind string

const (
	UninstallResultUninstalled      UninstallResultKind = "uninstalled"
	UninstallResultAlreadyAbsent    UninstallResultKind = "already-absent"
	UninstallResultConsentRequired  UninstallResultKind = "consent-required"
	UninstallResultPACRefreshFailed UninstallResultKind = "uninstalled-pac-refresh-failed"
)

type UninstallResult struct {
	Kind               UninstallResultKind  `json:"kind"`
	ConsentFingerprint string               `json:"consentFingerprint,omitempty"`
	Warnings           []HTTPSWarningDetail `json:"warnings,omitempty"`
}

type UninstallRequest struct {
	ConsentFingerprint string `json:"consentFingerprint,omitempty"`
}

type GatewayStatusKind string

const (
	GatewayStatusNotRunning GatewayStatusKind = "not-running"
	GatewayStatusStaleCache GatewayStatusKind = "stale-cache"
	GatewayStatusRouterOnly GatewayStatusKind = "router-only"
	GatewayStatusEnding     GatewayStatusKind = "ending"
	GatewayStatusStarting   GatewayStatusKind = "starting"
	GatewayStatusRunning    GatewayStatusKind = "running"
)

type StatusResult struct {
	Kind        GatewayStatusKind       `json:"kind"`
	Owner       *OwnerStatusDetail      `json:"owner,omitempty"`
	Runtime     *RuntimeStatusDetail    `json:"runtime,omitempty"`
	Cleanup     CleanupStatusDetail     `json:"cleanup"`
	InstalledCA InstalledCAStatusDetail `json:"installedCA"`
}

type OwnerStatusDetail struct {
	RouterListen string `json:"routerListen"`
}

type RuntimeStatusDetail struct {
	ProxyListen          string                      `json:"proxyListen"`
	PACListen            string                      `json:"pacListen"`
	UpstreamListPath     string                      `json:"upstreamListPath"`
	UpstreamCount        int                         `json:"upstreamCount"`
	UpstreamListWarnings []UpstreamListWarningDetail `json:"upstreamListWarnings,omitempty"`
	HTTPSReadiness       HTTPSReadinessStatus        `json:"httpsReadiness"`
	HTTPSInterception    HTTPSInterceptionState      `json:"httpsInterception"`
	HTTPSIntent          bool                        `json:"httpsIntent"`
	HTTPSWarnings        []HTTPSWarningDetail        `json:"httpsWarnings,omitempty"`
	ManagedPACServices   []string                    `json:"managedPacServices,omitempty"`
}

type HTTPSReadinessStatus string

const (
	HTTPSReadinessReady    HTTPSReadinessStatus = "ready"
	HTTPSReadinessNotReady HTTPSReadinessStatus = "not-ready"
)

type HTTPSInterceptionState string

const (
	HTTPSInterceptionInactive HTTPSInterceptionState = "inactive"
	HTTPSInterceptionActive   HTTPSInterceptionState = "active"
	HTTPSInterceptionFailed   HTTPSInterceptionState = "failed"
)

type HTTPSWarningKind string

const (
	HTTPSWarningUnmetIntent          HTTPSWarningKind = "unmet-https-intent"
	HTTPSWarningReadinessUnavailable HTTPSWarningKind = "https-readiness-unavailable"
	HTTPSWarningRenewalRecommended   HTTPSWarningKind = "userca-renewal-recommended"
	HTTPSWarningInterceptionFailed   HTTPSWarningKind = "https-interception-failed"
	HTTPSWarningPACRefreshFailed     HTTPSWarningKind = "https-pac-refresh-failed"
	HTTPSWarningUninstallIncomplete  HTTPSWarningKind = "userca-uninstall-incomplete"
)

type HTTPSWarningDetail struct {
	Kind       HTTPSWarningKind `json:"kind"`
	Diagnostic string           `json:"diagnostic"`
	Action     string           `json:"action,omitempty"`
}

type CleanupStatusDetail struct {
	State    CleanupStatusState           `json:"state"`
	Subjects []CleanupSubjectStatusDetail `json:"subjects"`
}

type CleanupStatusState string

const (
	CleanupStatusNone    CleanupStatusState = "none"
	CleanupStatusNeeded  CleanupStatusState = "needed"
	CleanupStatusUnknown CleanupStatusState = "unknown"
)

type CleanupSubjectStatusDetail struct {
	Subject    CleanupSubjectKind `json:"subject"`
	State      CleanupStatusState `json:"state"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

type InstalledCAStatusDetail struct {
	Health  CAHealthStatus `json:"health"`
	Expires time.Time      `json:"expires,omitempty"`
}

type CAHealthStatus string

const (
	CAHealthUsable    CAHealthStatus = "usable"
	CAHealthNotUsable CAHealthStatus = "not-usable"
	CAHealthMutating  CAHealthStatus = "mutating"
)

type lifecycle struct {
	mu                   sync.Mutex
	caAdmissionMu        sync.Mutex
	pacRefreshMu         sync.Mutex
	managedPACSettings   managedpac.SystemSettings
	userCA               userCAModule
	userCASnapshot       userCASnapshot
	userCAAssessmentErr  error
	coord                *coordinator
	runtimeDir           string
	routerListen         string
	ownerCache           stateCache
	startMutating        bool
	startCleanupComplete bool
	startCancel          context.CancelFunc
	startDone            chan struct{}
	ownerEnding          bool
	transientOwner       bool
	caMutating           bool
	runtime              *activeRuntime
	httpsWarningsChanged func([]HTTPSWarningDetail)
	fatal                chan error
}

type activeRuntime struct {
	engine *trafficRuntime
	pac    *managedpac.Session
	cancel context.CancelFunc
	done   chan error
	phase  runtimePhase
}

type runtimePhase string

const (
	runtimePhaseStarting runtimePhase = "starting"
	runtimePhaseRunning  runtimePhase = "running"
)

func newLifecycle(settings managedpac.SystemSettings, ca userCAModule, coord *coordinator, routerListen string) (*lifecycle, error) {
	return newLifecycleState(settings, ca, coord, routerListen, true)
}

func newLifecycleUninspected(settings managedpac.SystemSettings, ca userCAModule, coord *coordinator, routerListen string) (*lifecycle, error) {
	return newLifecycleState(settings, ca, coord, routerListen, false)
}

func newLifecycleState(
	settings managedpac.SystemSettings,
	ca userCAModule,
	coord *coordinator,
	routerListen string,
	inspectUserCA bool,
) (*lifecycle, error) {
	if coord == nil {
		var err error
		coord, err = defaultCoordinator()
		if err != nil {
			return nil, err
		}
	}
	if ca == nil {
		var err error
		ca, err = openSystemUserCA()
		if err != nil {
			return nil, err
		}
	}
	var initial userCASnapshot
	var assessmentErr error
	if inspectUserCA {
		initial, assessmentErr = ca.Inspect(context.Background())
	}
	return &lifecycle{
		managedPACSettings:  settings,
		userCA:              ca,
		userCASnapshot:      initial,
		userCAAssessmentErr: assessmentErr,
		coord:               coord,
		runtimeDir:          coord.RuntimeDirPath(),
		routerListen:        routerListen,
		fatal:               make(chan error, 1),
	}, nil
}

func (f *lifecycle) FatalRuntimeErrors() <-chan error {
	return f.fatal
}

func (f *lifecycle) RuntimeActive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runtime != nil
}

func (f *lifecycle) SetOwnerCache(cache stateCache) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ownerCache = cache
}

func (f *lifecycle) SetHTTPSWarningsChanged(publish func([]HTTPSWarningDetail)) {
	f.mu.Lock()
	f.httpsWarningsChanged = publish
	active := f.runtime
	f.mu.Unlock()
	if publish != nil && active != nil {
		publish(active.engine.snapshot().HTTPSWarnings)
	}
}

// MarkStartCleanupComplete records direct-start cleanup performed before the
// owner claimed its cache. Router-hosted starts deliberately do not use it.
func (f *lifecycle) MarkStartCleanupComplete() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCleanupComplete = true
}

func (f *lifecycle) takeStartCleanupComplete() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	complete := f.startCleanupComplete
	f.startCleanupComplete = false
	return complete
}

func (f *lifecycle) ExecuteStart(ctx context.Context, request StartRequest) (StartResult, error) {
	f.mu.Lock()
	if f.ownerEnding {
		f.mu.Unlock()
		return StartResult{Kind: StartResultStopCancelled}, nil
	}
	if f.transientOwner {
		f.mu.Unlock()
		return StartResult{Kind: StartResultStartAlreadyMutating}, nil
	}
	if f.runtime != nil {
		f.mu.Unlock()
		return StartResult{Kind: StartResultAlreadyRunning}, nil
	}
	if f.startMutating {
		f.mu.Unlock()
		return StartResult{Kind: StartResultStartAlreadyMutating}, nil
	}
	if f.caMutating {
		f.mu.Unlock()
		return StartResult{Kind: StartResultStartAlreadyMutating}, nil
	}
	f.startMutating = true
	startCtx, cancel := context.WithCancel(ctx)
	f.startCancel = cancel
	f.startDone = make(chan struct{})
	done := f.startDone
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.startMutating = false
		f.startCancel = nil
		f.startDone = nil
		f.mu.Unlock()
		close(done)
	}()

	return startSequence{lifecycle: f}.Execute(startCtx, request)
}

func (f *lifecycle) Stop(ctx context.Context) (StopResult, error) {
	var warnings []CommandWarning
	f.mu.Lock()
	f.ownerEnding = true
	startCancel := f.startCancel
	startDone := f.startDone
	active := f.runtime
	f.runtime = nil
	ownerCache := f.ownerCache
	f.mu.Unlock()
	if startCancel != nil {
		startCancel()
	}
	if active != nil {
		if active.pac != nil {
			active.pac.Close()
		}
		if err := active.engine.CloseTraffic(); err != nil {
			warnings = append(warnings, CommandWarning{Kind: CommandWarningRuntimeCloseFailed, Diagnostic: err.Error()})
		}
		active.cancel()
	}
	if startDone != nil {
		<-startDone
	}
	// Stop closes traffic first, then waits for owner-owned CA work.
	f.caAdmissionMu.Lock()
	f.caAdmissionMu.Unlock()
	var ownedCache *stateCache
	if ownerCache.HTTPRouterListen != "" && ownerCache.Token != "" {
		ownedCache = &ownerCache
	}
	cleanupFailures := cleanGatewayFootprint(ctx, f.managedPACSettings, f.coord, ownedCache)
	if len(cleanupFailures) > 0 {
		return StopResult{
			Kind:            StopResultCleanupFailed,
			Warnings:        warnings,
			CleanupFailures: cleanupFailures,
		}, nil
	}
	return StopResult{Kind: StopResultStopped, Warnings: warnings}, nil
}

func (f *lifecycle) Status(ctx context.Context, stale bool) (StatusResult, error) {
	f.mu.Lock()
	active := f.runtime
	ownerCache := f.ownerCache
	ownerEnding := f.ownerEnding
	caSnapshot := f.userCASnapshot
	caAssessmentErr := f.userCAAssessmentErr
	caMutating := f.caMutating
	var phase runtimePhase
	var pac *managedpac.Session
	if active != nil {
		phase = active.phase
		pac = active.pac
	}
	f.mu.Unlock()
	result := StatusResult{
		Kind:        GatewayStatusNotRunning,
		Cleanup:     f.cleanupStatus(ctx, stale, active != nil, ownerCache),
		InstalledCA: installedCAStatus(caSnapshot, caAssessmentErr, caMutating),
	}
	if ownerEnding {
		result.Kind = GatewayStatusEnding
		if f.routerListen != "" {
			result.Owner = &OwnerStatusDetail{RouterListen: f.routerListen}
		}
		return result, nil
	}
	if active != nil {
		if phase == runtimePhaseRunning {
			if err := pac.RequireLease(ctx); err != nil {
				f.reportFatalRuntimeError(active, err)
				return StatusResult{}, err
			}
		} else {
			result.Kind = GatewayStatusStarting
		}
		state := active.engine.snapshot()
		if phase == runtimePhaseRunning {
			result.Kind = GatewayStatusRunning
		}
		result.Owner = &OwnerStatusDetail{RouterListen: f.routerListen}
		result.Runtime = &RuntimeStatusDetail{
			ProxyListen:          state.ProxyListen,
			PACListen:            state.PACListen,
			UpstreamListPath:     state.UpstreamList,
			UpstreamCount:        state.UpstreamCount,
			UpstreamListWarnings: state.UpstreamListWarnings,
			HTTPSReadiness:       state.HTTPSReadiness,
			HTTPSInterception:    state.HTTPSInterception,
			HTTPSIntent:          state.HTTPSIntent,
			HTTPSWarnings:        state.HTTPSWarnings,
			ManagedPACServices:   managedPACServices(pac),
		}
		return result, nil
	}
	if f.routerListen != "" {
		result.Kind = GatewayStatusRouterOnly
		result.Owner = &OwnerStatusDetail{RouterListen: f.routerListen}
	} else if stale {
		result.Kind = GatewayStatusStaleCache
	}
	return result, nil
}

func upstreamListWarningDetails(warnings []upstreamlist.Warning) []UpstreamListWarningDetail {
	details := make([]UpstreamListWarningDetail, 0, len(warnings))
	for _, warning := range warnings {
		details = append(details, UpstreamListWarningDetail{
			Line:       warning.Line,
			Text:       warning.Text,
			Diagnostic: warning.Diagnostic,
		})
	}
	return details
}

func (f *lifecycle) Install(ctx context.Context) (InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}
	if !f.caAdmissionMu.TryLock() {
		return InstallResult{}, errCAOperationInProgress
	}
	f.mu.Lock()
	ownerEnding := f.ownerEnding
	startMutating := f.startMutating
	if !ownerEnding && !startMutating {
		f.caMutating = true
	}
	f.mu.Unlock()
	if ownerEnding || startMutating {
		f.caAdmissionMu.Unlock()
		if startMutating {
			return InstallResult{}, errCAOperationInProgress
		}
		return InstallResult{}, errGatewayOwnerEnding
	}
	defer func() {
		f.mu.Lock()
		if f.transientOwner {
			f.ownerEnding = true
		}
		f.caMutating = false
		f.mu.Unlock()
		f.caAdmissionMu.Unlock()
	}()
	f.mu.Lock()
	active := f.runtime
	var pac *managedpac.Session
	if active != nil {
		pac = active.pac
	}
	f.mu.Unlock()
	// Once admitted, CA work belongs to the owner rather than the request.
	result, err := f.userCA.Install(context.Background())
	if err != nil {
		return InstallResult{}, err
	}
	current := result.current
	f.mu.Lock()
	f.userCASnapshot = current
	f.userCAAssessmentErr = nil
	stillLive := f.runtime == active && active != nil
	f.mu.Unlock()
	if stillLive {
		if _, recoveryErr := active.engine.RecoverHTTPS(current); recoveryErr != nil {
			return InstallResult{
				Kind:               InstallResultRuntimeAdoptionFailed,
				InstalledCAExpires: current.expiresAt,
				Warnings: []HTTPSWarningDetail{{
					Kind:       HTTPSWarningInterceptionFailed,
					Diagnostic: fmt.Sprintf("Installed User CA is usable, but runtime adoption failed: %v.", recoveryErr),
					Action:     "Run `seamless-cors install` again.",
				}},
			}, nil
		}
	}
	if stillLive {
		if pac != nil {
			if refreshErr := f.refreshPACToCurrent(context.Background(), active); refreshErr != nil {
				active.engine.SetPACRefreshError(refreshErr)
				return InstallResult{
					Kind:               InstallResultPACRefreshFailed,
					InstalledCAExpires: current.expiresAt,
					Warnings: []HTTPSWarningDetail{{
						Kind:       HTTPSWarningPACRefreshFailed,
						Diagnostic: fmt.Sprintf("Installed User CA is usable, but HTTPS routing refresh failed: %v.", refreshErr),
						Action:     "Run `seamless-cors install` again.",
					}},
				}, nil
			}
			active.engine.SetPACRefreshError(nil)
		}
	}
	kind := InstallResultAlreadyUsable
	if result.changed {
		kind = InstallResultInstalled
	}
	return InstallResult{
		Kind:               kind,
		InstalledCAExpires: current.expiresAt,
	}, nil
}

func (f *lifecycle) Uninstall(ctx context.Context) (UninstallResult, error) {
	return f.UninstallWithConsent(ctx, "")
}

func (f *lifecycle) UninstallWithConsent(ctx context.Context, consentFingerprint string) (UninstallResult, error) {
	if err := ctx.Err(); err != nil {
		return UninstallResult{}, err
	}
	if !f.caAdmissionMu.TryLock() {
		return UninstallResult{}, errCAOperationInProgress
	}
	f.mu.Lock()
	if f.ownerEnding || f.startMutating {
		ending := f.ownerEnding
		f.mu.Unlock()
		f.caAdmissionMu.Unlock()
		if ending {
			return UninstallResult{}, errGatewayOwnerEnding
		}
		return UninstallResult{}, errCAOperationInProgress
	}
	active := f.runtime
	if active != nil && active.engine.snapshot().HTTPSInterception == HTTPSInterceptionActive {
		expected := f.uninstallConsentFingerprint(active)
		if consentFingerprint != expected {
			f.mu.Unlock()
			f.caAdmissionMu.Unlock()
			return UninstallResult{
				Kind:               UninstallResultConsentRequired,
				ConsentFingerprint: expected,
			}, nil
		}
	}
	f.caMutating = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		if f.transientOwner {
			f.ownerEnding = true
		}
		f.caMutating = false
		f.mu.Unlock()
		f.caAdmissionMu.Unlock()
	}()
	var pacRefreshErr error
	if active != nil {
		active.engine.DeactivateHTTPS(userCASnapshot{})
	}
	result, err := f.userCA.Uninstall(context.Background())
	if err != nil {
		f.mu.Lock()
		f.userCASnapshot = userCASnapshot{}
		f.userCAAssessmentErr = err
		f.mu.Unlock()
		if active != nil {
			active.engine.SetUninstallWarning(err)
		}
		return UninstallResult{}, err
	}
	current := result.current
	f.mu.Lock()
	f.userCASnapshot = current
	f.userCAAssessmentErr = nil
	stillLive := f.runtime == active && active != nil
	f.mu.Unlock()
	if stillLive && active.pac != nil {
		pacRefreshErr = f.refreshPACToCurrent(context.Background(), active)
		active.engine.SetPACRefreshError(pacRefreshErr)
	}
	if pacRefreshErr != nil {
		return UninstallResult{
			Kind: UninstallResultPACRefreshFailed,
			Warnings: []HTTPSWarningDetail{{
				Kind:       HTTPSWarningPACRefreshFailed,
				Diagnostic: fmt.Sprintf("HTTPS routing refresh failed: %v.", pacRefreshErr),
				Action:     "Run `seamless-cors uninstall` again to reconcile routing.",
			}},
		}, nil
	}
	if !result.changed {
		return UninstallResult{Kind: UninstallResultAlreadyAbsent}, nil
	}
	return UninstallResult{Kind: UninstallResultUninstalled}, nil
}

func (f *lifecycle) uninstallConsentFingerprint(active *activeRuntime) string {
	state := active.engine.snapshot()
	sum := sha256.Sum256([]byte(state.ProxyListen + "\x00" + state.PACListen + "\x00uninstall-all-usercas"))
	return hex.EncodeToString(sum[:])
}

func (f *lifecycle) watchPACRefreshes(ctx context.Context, active *activeRuntime) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-active.engine.PACURLUpdates():
			if err := f.refreshPACToCurrent(ctx, active); err != nil {
				active.engine.SetPACRefreshError(err)
				continue
			}
			active.engine.SetPACRefreshError(nil)
		}
	}
}

func (f *lifecycle) watchHTTPSWarningUpdates(ctx context.Context, active *activeRuntime) {
	for {
		select {
		case <-ctx.Done():
			return
		case warnings := <-active.engine.HTTPSWarningUpdates():
			f.mu.Lock()
			publish := f.httpsWarningsChanged
			stillActive := f.runtime == active
			f.mu.Unlock()
			if publish != nil && stillActive {
				publish(append([]HTTPSWarningDetail(nil), warnings...))
			}
		}
	}
}

func (f *lifecycle) refreshPACToCurrent(ctx context.Context, active *activeRuntime) error {
	f.pacRefreshMu.Lock()
	defer f.pacRefreshMu.Unlock()
	if active.pac == nil {
		return nil
	}
	nextURL := active.engine.PACURL()
	if active.pac.CurrentURL() == nextURL {
		return nil
	}
	return active.pac.Refresh(ctx, nextURL)
}

func (f *lifecycle) reportFatalRuntimeError(active *activeRuntime, err error) {
	f.mu.Lock()
	if f.runtime == active {
		active.cancel()
	}
	f.mu.Unlock()
	select {
	case f.fatal <- err:
	default:
	}
}

func (f *lifecycle) pacReplacementConsentDetail(assessment managedpac.Assessment) *PACReplacementConsentDetail {
	out := make([]ManagedPACServiceState, 0, len(assessment.Services))
	for _, state := range assessment.Services {
		out = append(out, ManagedPACServiceState{
			ServiceName:                state.ServiceName,
			Enabled:                    state.Enabled,
			URL:                        state.PACURL,
			Ownership:                  pacOwnership(state.Ownership),
			ReplacementConsentRequired: state.Ownership == managedpac.OwnershipForeign,
		})
	}
	return &PACReplacementConsentDetail{
		CurrentPACState: out,
		CleanupMode:     CleanupModeNoPACRestoration,
		Fingerprint:     pacConsentFingerprint(assessment.Services),
	}
}

func pacConsentFingerprint(states []managedpac.ServiceAssessment) PACConsentFingerprint {
	h := sha256.New()
	var size [8]byte
	for _, state := range states {
		if state.Ownership != managedpac.OwnershipForeign {
			continue
		}
		for _, value := range []string{state.ServiceName, state.PACURL} {
			binary.BigEndian.PutUint64(size[:], uint64(len(value)))
			_, _ = h.Write(size[:])
			_, _ = h.Write([]byte(value))
		}
	}
	return PACConsentFingerprint(hex.EncodeToString(h.Sum(nil)))
}

func managedPACServices(pac *managedpac.Session) []string {
	if pac == nil {
		return nil
	}
	return pac.Services()
}

func pacOwnership(ownership managedpac.Ownership) PACOwnership {
	switch ownership {
	case managedpac.OwnershipEmpty:
		return PACOwnershipEmpty
	case managedpac.OwnershipOwned:
		return PACOwnershipOwned
	default:
		return PACOwnershipForeign
	}
}

func (f *lifecycle) cleanupStatus(ctx context.Context, stale bool, runtimeActive bool, ownerCache stateCache) CleanupStatusDetail {
	return inspectGatewayFootprint(ctx, f.managedPACSettings, f.coord, stale, runtimeActive, ownerCache)
}

func installedCAStatus(snapshot userCASnapshot, assessmentErr error, mutating bool) InstalledCAStatusDetail {
	if mutating {
		return InstalledCAStatusDetail{Health: CAHealthMutating}
	}
	if assessmentErr != nil || !snapshot.usable {
		return InstalledCAStatusDetail{Health: CAHealthNotUsable}
	}
	return InstalledCAStatusDetail{Health: CAHealthUsable, Expires: snapshot.expiresAt}
}

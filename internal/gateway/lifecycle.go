package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

var (
	errOwnerTransition = errors.New("gateway ownership is transitioning")
)

// StartKind identifies the semantic outcome of a Start operation.  Start
// outcomes are deliberately scoped to Start; they are not shared with the
// other Gateway commands.
type StartKind string

type CommandFulfillment string

const (
	CommandFulfilled   CommandFulfillment = "fulfilled"
	CommandUnfulfilled CommandFulfillment = "unfulfilled"
)

const (
	StartResultStarted                             StartKind = "started"
	StartResultAlreadyRunning                      StartKind = "already-running"
	StartResultOwnerTransition                     StartKind = "owner-transition"
	StartResultUpstreamListCreationConsentRequired StartKind = "upstream-list-creation-consent-required"
	StartResultConsentRequired                     StartKind = "managed-pac-consent-required"
	StartResultConsentDeclined                     StartKind = "managed-pac-consent-declined"
	StartResultNoManageablePACServices             StartKind = "no-manageable-pac-services"
	StartResultManagedPACInstallationFailed        StartKind = "managed-pac-installation-failed"
	StartResultStartAlreadyMutating                StartKind = "start-already-mutating"
	StartResultStopCancelled                       StartKind = "stop-cancelled"
	StartResultCleanupFailed                       StartKind = "cleanup-failed"
)

type StartRequest struct {
	UpstreamListCreationConsent *UpstreamListCreationConsentInput `json:"upstreamListCreationConsent,omitempty"`
	ManagedPACConsent           *ManagedPACConsentInput           `json:"managedPacConsent,omitempty"`
}

// StartResult is the closed semantic result of a Start operation.  Concrete
// variants carry only the payload that is legal for that outcome.
type StartResult interface {
	Kind() StartKind
	Fulfillment() CommandFulfillment
	UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail
	startResult()
}

// Started reports that the runtime was started and includes the initial
// guidance snapshot.
type Started struct {
	Guidance                    StartGuidance
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

// AlreadyRunning reports that a runtime was already active.
type AlreadyRunning struct{}

// StartOwnerTransition reports that ownership is being acquired or released.
type StartOwnerTransition struct{}

type StartUpstreamListCreationConsentRequired struct{ Consent UpstreamListCreationConsent }

type UpstreamListCreationConsent struct {
	Path                     string                          `json:"path"`
	DefaultContents          string                          `json:"defaultContents"`
	MissingParentDirectories []string                        `json:"missingParentDirectories,omitempty"`
	Fingerprint              UpstreamListCreationFingerprint `json:"fingerprint"`
}

type UpstreamListCreationDecision string

const (
	UpstreamListCreationAccepted UpstreamListCreationDecision = "accepted"
	UpstreamListCreationDeclined UpstreamListCreationDecision = "declined"
)

type UpstreamListCreationFingerprint string
type UpstreamListCreationConsentInput struct {
	Decision    UpstreamListCreationDecision    `json:"decision"`
	Fingerprint UpstreamListCreationFingerprint `json:"fingerprint,omitempty"`
}

// StartConsentRequired reports the managed PAC services requiring confirmation.
type StartConsentRequired struct {
	Consent                     ManagedPACConsent
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

// StartConsentDeclined reports that managed PAC consent was declined.
type StartConsentDeclined struct{}

// StartNoManageablePACServices reports that managed PAC inspection found no
// services that can be managed.  Consent contains the inspection snapshot.
type StartNoManageablePACServices struct {
	Consent                     ManagedPACConsent
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

// StartManagedPACInstallationFailed reports a failed PAC installation and any
// warnings produced while attempting it.
type StartManagedPACInstallationFailed struct {
	Warnings                    []ManagedPACWarningDetail
	Diagnostic                  string
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

// StartAlreadyMutating reports that another start/CA mutation is in progress.
type StartAlreadyMutating struct {
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

// StartStopCancelled reports that Stop cancelled the Start operation.
type StartStopCancelled struct {
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

// StartCleanupFailed reports cleanup failures.  Warnings are retained when
// they were produced by a failed Managed PAC installation before cleanup.
type StartCleanupFailed struct {
	Failures                    []CleanupFailure
	Warnings                    []ManagedPACWarningDetail
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

type UpstreamListCreationWarningDetail struct {
	Cause string `json:"cause"`
}

func startFulfillment(kind StartKind) CommandFulfillment {
	if kind == StartResultStarted || kind == StartResultAlreadyRunning {
		return CommandFulfilled
	}
	return CommandUnfulfilled
}

func (Started) Kind() StartKind                 { return StartResultStarted }
func (Started) Fulfillment() CommandFulfillment { return startFulfillment(StartResultStarted) }
func (r Started) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (Started) startResult()           {}
func (AlreadyRunning) Kind() StartKind { return StartResultAlreadyRunning }
func (AlreadyRunning) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultAlreadyRunning)
}
func (AlreadyRunning) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return nil
}
func (AlreadyRunning) startResult()          {}
func (StartOwnerTransition) Kind() StartKind { return StartResultOwnerTransition }
func (StartOwnerTransition) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultOwnerTransition)
}
func (StartOwnerTransition) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return nil
}
func (StartOwnerTransition) startResult() {}
func (StartUpstreamListCreationConsentRequired) Kind() StartKind {
	return StartResultUpstreamListCreationConsentRequired
}
func (StartUpstreamListCreationConsentRequired) Fulfillment() CommandFulfillment {
	return CommandUnfulfilled
}
func (StartUpstreamListCreationConsentRequired) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return nil
}
func (StartUpstreamListCreationConsentRequired) startResult() {}
func (StartConsentRequired) Kind() StartKind                  { return StartResultConsentRequired }
func (StartConsentRequired) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultConsentRequired)
}
func (r StartConsentRequired) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (StartConsentRequired) startResult()    {}
func (StartConsentDeclined) Kind() StartKind { return StartResultConsentDeclined }
func (StartConsentDeclined) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultConsentDeclined)
}
func (StartConsentDeclined) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return nil
}
func (StartConsentDeclined) startResult() {}
func (StartNoManageablePACServices) Kind() StartKind {
	return StartResultNoManageablePACServices
}
func (StartNoManageablePACServices) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultNoManageablePACServices)
}
func (r StartNoManageablePACServices) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (StartNoManageablePACServices) startResult() {}
func (StartManagedPACInstallationFailed) Kind() StartKind {
	return StartResultManagedPACInstallationFailed
}
func (StartManagedPACInstallationFailed) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultManagedPACInstallationFailed)
}
func (r StartManagedPACInstallationFailed) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (StartManagedPACInstallationFailed) startResult() {}
func (StartAlreadyMutating) Kind() StartKind           { return StartResultStartAlreadyMutating }
func (StartAlreadyMutating) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultStartAlreadyMutating)
}
func (r StartAlreadyMutating) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (StartAlreadyMutating) startResult()  {}
func (StartStopCancelled) Kind() StartKind { return StartResultStopCancelled }
func (StartStopCancelled) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultStopCancelled)
}
func (r StartStopCancelled) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (StartStopCancelled) startResult()    {}
func (StartCleanupFailed) Kind() StartKind { return StartResultCleanupFailed }
func (StartCleanupFailed) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultCleanupFailed)
}
func (r StartCleanupFailed) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (StartCleanupFailed) startResult() {}

type ManagedPACConsent struct {
	CurrentPACState  []ManagedPACServiceState `json:"currentPacState"`
	ProposedServices []string                 `json:"proposedServices"`
	CleanupMode      CleanupMode              `json:"cleanupMode"`
	Fingerprint      PACConsentFingerprint    `json:"fingerprint"`
}

// ManagedPACConsentDetail is retained as a representation-oriented alias for
// existing control-surface helpers; Start variants use ManagedPACConsent.
type ManagedPACConsentDetail = ManagedPACConsent

type CleanupMode string

const CleanupModeNoPACRestoration CleanupMode = "no-pac-restoration"

type ManagedPACServiceState struct {
	ServiceName string       `json:"serviceName"`
	Enabled     bool         `json:"enabled"`
	URL         string       `json:"url"`
	Ownership   PACOwnership `json:"ownership"`
	Manageable  bool         `json:"manageable"`
}

type PACOwnership string

const (
	PACOwnershipEmpty   PACOwnership = "empty"
	PACOwnershipOwned   PACOwnership = "owned"
	PACOwnershipForeign PACOwnership = "foreign"
)

type ManagedPACConsentInput struct {
	ServiceNames []string              `json:"serviceNames"`
	Fingerprint  PACConsentFingerprint `json:"fingerprint"`
}

type PACConsentFingerprint string

type StartGuidance struct {
	UpstreamListPath            string                       `json:"upstreamListPath"`
	ManagedPACActive            bool                         `json:"managedPacActive"`
	ManagedPACServices          []string                     `json:"managedPacServices,omitempty"`
	ManagedPACPublicationURL    string                       `json:"managedPacPublicationUrl,omitempty"`
	HTTPSPipeline               *HTTPSPipelineDetail         `json:"httpsPipeline,omitempty"`
	InstalledCA                 *InstalledCAStatusDetail     `json:"installedCA,omitempty"`
	ManagedPACWarnings          []ManagedPACWarningDetail    `json:"managedPacWarnings,omitempty"`
	UpstreamListWarnings        []UpstreamListWarningDetail  `json:"upstreamListWarnings,omitempty"`
	UpstreamListFileSyncIssue   *FileSyncIssue               `json:"upstreamListFileSyncIssue,omitempty"`
	UpstreamListProjectionIssue *UpstreamListProjectionIssue `json:"upstreamListProjectionIssue,omitempty"`
}

// StartGuidanceDetail is retained as a representation-oriented alias for
// existing control-surface helpers; Started uses StartGuidance.
type StartGuidanceDetail = StartGuidance

type UpstreamListWarningDetail struct {
	Line       int    `json:"line"`
	Text       string `json:"text"`
	Diagnostic string `json:"diagnostic"`
}

type FileSyncIssueKind string

const (
	FileSyncIssueFileUnreadable     FileSyncIssueKind = "file-unreadable"
	FileSyncIssueObservationStopped FileSyncIssueKind = "observation-stopped"
)

type FileSyncIssue struct {
	Kind  FileSyncIssueKind `json:"kind"`
	Cause string            `json:"cause"`
}

type UpstreamListProjectionIssue struct {
	Cause string `json:"cause"`
}

type StopResultKind string

const (
	StopResultStopped                 StopResultKind = "stopped"
	StopResultNotRunning              StopResultKind = "not-running"
	StopResultCleanupFailed           StopResultKind = "cleanup-failed"
	StopResultNotRunningCleanupFailed StopResultKind = "not-running-cleanup-failed"
)

type StopResult struct {
	Kind            StopResultKind
	Warnings        []CommandWarning
	CleanupFailures []CleanupFailureDetail
}

func (r StopResult) Fulfillment() CommandFulfillment {
	if r.Kind == StopResultStopped || r.Kind == StopResultNotRunning {
		return CommandFulfilled
	}
	return CommandUnfulfilled
}

type CleanupFailure struct {
	Subject    CleanupSubjectKind `json:"subject"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

// CleanupFailureDetail is retained as a representation-oriented alias for
// existing stop and cleanup surfaces; StartCleanupFailed uses CleanupFailure.
type CleanupFailureDetail = CleanupFailure

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
	InstallResultInstalled       InstallResultKind = "installed"
	InstallResultAlreadyUsable   InstallResultKind = "already-usable"
	InstallResultApprovalDenied  InstallResultKind = "approval-denied"
	InstallResultAlreadyMutating InstallResultKind = "already-mutating"
	InstallResultOwnerEnding     InstallResultKind = "owner-ending"
	InstallResultOwnerTransition InstallResultKind = "owner-transition"
)

type InstallResult struct {
	Kind               InstallResultKind
	InstalledCAExpires time.Time
	HTTPSPipeline      *HTTPSPipelineDetail
}

func (r InstallResult) Fulfillment() CommandFulfillment {
	if r.Kind == InstallResultInstalled || r.Kind == InstallResultAlreadyUsable {
		return CommandFulfilled
	}
	return CommandUnfulfilled
}

type UninstallResultKind string

const (
	UninstallResultUninstalled     UninstallResultKind = "uninstalled"
	UninstallResultAlreadyAbsent   UninstallResultKind = "already-absent"
	UninstallResultConsentRequired UninstallResultKind = "consent-required"
	UninstallResultAlreadyMutating UninstallResultKind = "already-mutating"
	UninstallResultOwnerEnding     UninstallResultKind = "owner-ending"
	UninstallResultOwnerTransition UninstallResultKind = "owner-transition"
	UninstallResultIncomplete      UninstallResultKind = "incomplete"
)

type UninstallResult struct {
	Kind               UninstallResultKind
	ConsentFingerprint string
	CleanupIssue       *UserCACleanupIssue
}

func (r UninstallResult) Fulfillment() CommandFulfillment {
	if r.Kind == UninstallResultUninstalled || r.Kind == UninstallResultAlreadyAbsent {
		return CommandFulfilled
	}
	return CommandUnfulfilled
}

type UninstallRequest struct {
	ConsentFingerprint string `json:"consentFingerprint,omitempty"`
}

type GatewayStatusKind string

type StatusResultKind string

const (
	StatusResultReported        StatusResultKind = "reported"
	StatusResultOwnerTransition StatusResultKind = "owner-transition"
)

const (
	GatewayStatusNotRunning GatewayStatusKind = "not-running"
	GatewayStatusStaleCache GatewayStatusKind = "stale-cache"
	GatewayStatusRouterOnly GatewayStatusKind = "router-only"
	GatewayStatusEnding     GatewayStatusKind = "ending"
	GatewayStatusStarting   GatewayStatusKind = "starting"
	GatewayStatusRunning    GatewayStatusKind = "running"
)

type StatusResult struct {
	Kind StatusResultKind
	StatusReport
}

type StatusReport struct {
	State       GatewayStatusKind       `json:"state"`
	Owner       *OwnerStatusDetail      `json:"owner,omitempty"`
	Runtime     *RuntimeStatusDetail    `json:"runtime,omitempty"`
	Cleanup     CleanupStatusDetail     `json:"cleanup"`
	InstalledCA InstalledCAStatusDetail `json:"installedCA"`
}

func (r StatusResult) Fulfillment() CommandFulfillment {
	if r.Kind == StatusResultReported {
		return CommandFulfilled
	}
	return CommandUnfulfilled
}

type OwnerStatusDetail struct {
	RouterListen string `json:"routerListen"`
}

type RuntimeStatusDetail struct {
	ProxyListen                 string                       `json:"proxyListen"`
	PACListen                   string                       `json:"pacListen"`
	UpstreamListPath            string                       `json:"upstreamListPath"`
	UpstreamCount               int                          `json:"upstreamCount"`
	UpstreamListWarnings        []UpstreamListWarningDetail  `json:"upstreamListWarnings,omitempty"`
	UpstreamListFileSyncIssue   *FileSyncIssue               `json:"upstreamListFileSyncIssue,omitempty"`
	UpstreamListProjectionIssue *UpstreamListProjectionIssue `json:"upstreamListProjectionIssue,omitempty"`
	HTTPSPipeline               *HTTPSPipelineDetail         `json:"httpsPipeline,omitempty"`
	ManagedPACActive            bool                         `json:"managedPacActive"`
	ManagedPACServices          []string                     `json:"managedPacServices,omitempty"`
	ManagedPACPublicationURL    string                       `json:"managedPacPublicationUrl,omitempty"`
	ManagedPACWarnings          []ManagedPACWarningDetail    `json:"managedPacWarnings,omitempty"`
}

type HTTPSReadinessStatus string

const (
	HTTPSReadinessReady    HTTPSReadinessStatus = "ready"
	HTTPSReadinessNotReady HTTPSReadinessStatus = "not-ready"
)

type HTTPSPipelinePhase string

const (
	HTTPSPipelineAssessing HTTPSPipelinePhase = "assessing"
	HTTPSPipelineSettled   HTTPSPipelinePhase = "settled"
)

type HTTPSPipelineDetail struct {
	Phase                 HTTPSPipelinePhase      `json:"phase"`
	Readiness             HTTPSReadinessStatus    `json:"readiness,omitempty"`
	UnmetIntent           *UnmetHTTPSIntentDetail `json:"unmetIntent,omitempty"`
	UserCAAssessmentIssue *UserCAAssessmentIssue  `json:"userCAAssessmentIssue,omitempty"`
}

type UnmetHTTPSIntentDetail struct {
	Diagnostic string `json:"diagnostic"`
	Action     string `json:"action,omitempty"`
}

type UserCAAssessmentIssue struct {
	Cause  string `json:"cause"`
	Action string `json:"action,omitempty"`
}

type UserCACleanupIssue struct {
	Cause  string `json:"cause"`
	Action string `json:"action,omitempty"`
}

type ManagedPACWarningKind string

const (
	ManagedPACWarningDrift        ManagedPACWarningKind = "drift"
	ManagedPACWarningUpdateFailed ManagedPACWarningKind = "update-failed"
)

type ManagedPACWarningDetail struct {
	Kind        ManagedPACWarningKind `json:"kind"`
	ServiceName string                `json:"serviceName,omitempty"`
	Diagnostic  string                `json:"diagnostic"`
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
	Health       CAHealthStatus      `json:"health"`
	Expires      time.Time           `json:"expires,omitempty"`
	RenewalDue   bool                `json:"renewalDue,omitempty"`
	CleanupIssue *UserCACleanupIssue `json:"cleanupIssue,omitempty"`
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
	managedPAC           managedPACModule
	userCA               userCAModule
	userCASnapshot       userca.Snapshot
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
	deadlinePending      bool
	pipelinePending      bool
	runtime              *activeRuntime
	httpsPipelineChanged func(*HTTPSPipelineDetail)
	userCACleanupIssue   *UserCACleanupIssue
	fatal                chan error
}

type activeRuntime struct {
	engine             *trafficRuntime
	managedPAC         *managedPACRuntime
	ctx                context.Context
	cancel             context.CancelFunc
	done               chan error
	phase              runtimePhase
	deadlineTimer      *time.Timer
	deadlineGeneration uint64
	assessmentCancel   context.CancelFunc
}

type managedPACRuntime struct {
	state    managedpac.RuntimeState
	warnings []ManagedPACWarningDetail
}

type runtimePhase string

const (
	runtimePhaseStarting runtimePhase = "starting"
	runtimePhaseRunning  runtimePhase = "running"
)

func newLifecycle(pac managedPACModule, ca userCAModule, coord *coordinator, routerListen string) (*lifecycle, error) {
	return newLifecycleState(pac, ca, coord, routerListen, true)
}

func newLifecycleUninspected(pac managedPACModule, ca userCAModule, coord *coordinator, routerListen string) (*lifecycle, error) {
	return newLifecycleState(pac, ca, coord, routerListen, false)
}

func newLifecycleState(
	pac managedPACModule,
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
	if pac == nil {
		pac = openSystemManagedPAC()
	}
	var initial userca.Assessment
	var assessmentErr error
	if inspectUserCA {
		initial, assessmentErr = ca.Inspect(context.Background())
	}
	return &lifecycle{
		managedPAC:          pac,
		userCA:              ca,
		userCASnapshot:      initial.Snapshot(),
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

func (f *lifecycle) SetHTTPSPipelineChanged(publish func(*HTTPSPipelineDetail)) {
	f.mu.Lock()
	f.httpsPipelineChanged = publish
	active := f.runtime
	f.mu.Unlock()
	if publish != nil && active != nil {
		publish(active.engine.snapshot().HTTPSPipeline)
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

func (f *lifecycle) scheduleHTTPSDeadline(active *activeRuntime, assessment userca.Assessment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runtime != active {
		return
	}
	f.scheduleHTTPSDeadlineLocked(active, assessment)
}

func (f *lifecycle) scheduleHTTPSDeadlineLocked(active *activeRuntime, assessment userca.Assessment) {
	snapshot := assessment.Snapshot()
	if !snapshot.Usable() {
		return
	}
	state := active.engine.snapshot()
	if state.HTTPSPipeline == nil || state.HTTPSPipeline.Readiness != HTTPSReadinessReady {
		return
	}
	if active.deadlineTimer != nil {
		active.deadlineTimer.Stop()
	}
	active.deadlineGeneration = state.HTTPSGeneration
	now := time.Now()
	delay := snapshot.ExpiresAt().Sub(now)
	if delay < 0 {
		delay = 0
	}
	generation := active.deadlineGeneration
	active.deadlineTimer = time.AfterFunc(delay, func() {
		f.handleHTTPSDeadline(active, generation)
	})
}

func (f *lifecycle) cancelHTTPSDeadline(active *activeRuntime) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if active.deadlineTimer != nil {
		active.deadlineTimer.Stop()
		active.deadlineTimer = nil
	}
	active.deadlineGeneration = 0
}

func (f *lifecycle) handleHTTPSDeadline(active *activeRuntime, generation uint64) {
	if !f.caAdmissionMu.TryLock() {
		f.mu.Lock()
		if f.runtime == active {
			f.deadlinePending = true
		}
		f.mu.Unlock()
		return
	}
	defer f.finishHTTPSAssessment(active)
	f.mu.Lock()
	if f.runtime != active {
		f.mu.Unlock()
		return
	}
	if f.caMutating || f.startMutating {
		f.deadlinePending = true
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()
	if !active.engine.interceptionActive() {
		f.cancelHTTPSDeadline(active)
		return
	}
	nextGeneration, ok := active.engine.BeginHTTPSDeadlineAssessment(generation)
	if !ok {
		return
	}
	f.assessHTTPSPipeline(active, nextGeneration)
}

func (f *lifecycle) requestHTTPSAssessment(active *activeRuntime, generation uint64) {
	if !f.caAdmissionMu.TryLock() {
		f.mu.Lock()
		if f.runtime == active {
			f.pipelinePending = true
		}
		f.mu.Unlock()
		return
	}
	go func() {
		defer f.finishHTTPSAssessment(active)
		f.assessHTTPSPipeline(active, generation)
	}()
}

func (f *lifecycle) finishHTTPSAssessment(active *activeRuntime) {
	f.caAdmissionMu.Unlock()
	f.mu.Lock()
	pending := (f.pipelinePending || f.deadlinePending) && f.runtime == active
	f.pipelinePending = false
	f.deadlinePending = false
	f.mu.Unlock()
	if pending {
		if generation, ok := active.engine.pendingHTTPSAssessment(); ok {
			f.requestHTTPSAssessment(active, generation)
		}
	}
}

func (f *lifecycle) assessHTTPSPipeline(active *activeRuntime, generation uint64) {
	f.mu.Lock()
	if f.runtime != active {
		f.mu.Unlock()
		return
	}
	if active.assessmentCancel != nil {
		active.assessmentCancel()
	}
	ctx := active.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	assessmentCtx, cancel := context.WithCancel(ctx)
	active.assessmentCancel = cancel
	f.mu.Unlock()

	assessment, assessmentErr := f.userCA.Inspect(assessmentCtx)
	applied, ready := active.engine.settleHTTPSAssessment(generation, assessment, assessmentErr)

	f.mu.Lock()
	active.assessmentCancel = nil
	stillActive := f.runtime == active
	if applied && stillActive {
		f.userCASnapshot = assessment.Snapshot()
		f.userCAAssessmentErr = assessmentErr
	}
	f.mu.Unlock()
	cancel()
	if !applied || !stillActive {
		return
	}
	if ready {
		f.scheduleHTTPSDeadline(active, assessment)
	} else {
		f.cancelHTTPSDeadline(active)
	}
}

func (f *lifecycle) finishCAMutation(active *activeRuntime) {
	f.mu.Lock()
	if f.transientOwner {
		f.ownerEnding = true
	}
	f.caMutating = false
	pending := (f.deadlinePending || f.pipelinePending) && f.runtime == active && active != nil
	f.deadlinePending = false
	f.pipelinePending = false
	f.mu.Unlock()
	f.caAdmissionMu.Unlock()
	if pending {
		if generation, ok := active.engine.pendingHTTPSAssessment(); ok {
			f.requestHTTPSAssessment(active, generation)
		}
	}
}

func (f *lifecycle) ExecuteStart(ctx context.Context, request StartRequest) (StartResult, error) {
	f.mu.Lock()
	if f.ownerEnding {
		f.mu.Unlock()
		return StartStopCancelled{}, nil
	}
	if f.transientOwner {
		f.mu.Unlock()
		return StartAlreadyMutating{}, nil
	}
	if f.runtime != nil {
		f.mu.Unlock()
		return AlreadyRunning{}, nil
	}
	if f.startMutating {
		f.mu.Unlock()
		return StartAlreadyMutating{}, nil
	}
	if f.caMutating {
		f.mu.Unlock()
		return StartAlreadyMutating{}, nil
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
		active := f.runtime
		pending := (f.deadlinePending || f.pipelinePending) && active != nil
		f.deadlinePending = false
		f.pipelinePending = false
		f.mu.Unlock()
		if pending {
			if generation, ok := active.engine.pendingHTTPSAssessment(); ok {
				f.requestHTTPSAssessment(active, generation)
			}
		}
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
	var assessmentCancel context.CancelFunc
	if active != nil {
		assessmentCancel = active.assessmentCancel
	}
	f.runtime = nil
	ownerCache := f.ownerCache
	f.mu.Unlock()
	if startCancel != nil {
		startCancel()
	}
	if active != nil {
		f.cancelHTTPSDeadline(active)
		active.engine.invalidateHTTPSAssessments()
		if assessmentCancel != nil {
			assessmentCancel()
		}
		if err := active.engine.CloseTraffic(); err != nil {
			warnings = append(warnings, CommandWarning{Kind: CommandWarningRuntimeCloseFailed, Diagnostic: err.Error()})
		}
		active.cancel()
	}
	if startDone != nil {
		<-startDone
	}
	// Once traffic and any preempted Start have settled, wait for admitted
	// owner-owned CA work before durable footprint cleanup.
	f.caAdmissionMu.Lock()
	f.caAdmissionMu.Unlock()
	var cleanupFailures []CleanupFailureDetail
	if failure := uninstallManagedPAC(ctx, f.managedPAC); failure != nil {
		cleanupFailures = append(cleanupFailures, *failure)
	}
	var ownedCache *stateCache
	if ownerCache.HTTPRouterListen != "" && ownerCache.Token != "" {
		ownedCache = &ownerCache
	}
	cleanupFailures = append(cleanupFailures, cleanGatewayStateCache(f.coord, ownedCache)...)
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
	caCleanupIssue := f.userCACleanupIssue
	var phase runtimePhase
	var managedPACActive bool
	var managedPACServiceNames []string
	var managedPACPublicationURL string
	var managedPACWarningSnapshot []ManagedPACWarningDetail
	if active != nil {
		phase = active.phase
		if active.managedPAC != nil {
			managedPACActive = true
			managedPACServiceNames = active.managedPAC.state.ServiceNames()
			managedPACPublicationURL = active.managedPAC.state.PACURL()
			managedPACWarningSnapshot = append([]ManagedPACWarningDetail(nil), active.managedPAC.warnings...)
		}
	}
	f.mu.Unlock()
	result := StatusResult{
		Kind: StatusResultReported,
		StatusReport: StatusReport{
			State:       GatewayStatusNotRunning,
			Cleanup:     f.cleanupStatus(ctx, stale, active != nil, ownerCache),
			InstalledCA: installedCAStatus(caSnapshot, caAssessmentErr, caMutating, caCleanupIssue),
		},
	}
	if ownerEnding {
		result.State = GatewayStatusEnding
		if f.routerListen != "" {
			result.Owner = &OwnerStatusDetail{RouterListen: f.routerListen}
		}
		return result, nil
	}
	if active != nil {
		if phase != runtimePhaseRunning {
			result.State = GatewayStatusStarting
		}
		state := active.engine.snapshot()
		if phase == runtimePhaseRunning {
			result.State = GatewayStatusRunning
		}
		result.Owner = &OwnerStatusDetail{RouterListen: f.routerListen}
		result.Runtime = &RuntimeStatusDetail{
			ProxyListen:                 state.ProxyListen,
			PACListen:                   state.PACListen,
			UpstreamListPath:            state.UpstreamList,
			UpstreamCount:               state.UpstreamCount,
			UpstreamListWarnings:        state.UpstreamListWarnings,
			UpstreamListFileSyncIssue:   state.UpstreamListFileSyncIssue,
			UpstreamListProjectionIssue: state.UpstreamListProjectionIssue,
			HTTPSPipeline:               state.HTTPSPipeline,
			ManagedPACActive:            managedPACActive,
			ManagedPACServices:          managedPACServiceNames,
			ManagedPACPublicationURL:    managedPACPublicationURL,
			ManagedPACWarnings:          managedPACWarningSnapshot,
		}
		return result, nil
	}
	if f.routerListen != "" {
		result.State = GatewayStatusRouterOnly
		result.Owner = &OwnerStatusDetail{RouterListen: f.routerListen}
	} else if stale {
		result.State = GatewayStatusStaleCache
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
		return InstallResult{Kind: InstallResultAlreadyMutating}, nil
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
			return InstallResult{Kind: InstallResultAlreadyMutating}, nil
		}
		return InstallResult{Kind: InstallResultOwnerEnding}, nil
	}
	f.mu.Lock()
	active := f.runtime
	f.mu.Unlock()
	defer f.finishCAMutation(active)
	// Once admitted, CA work belongs to the owner rather than the request.
	result, err := f.userCA.Install(context.Background())
	if err != nil {
		if errors.Is(err, userca.ErrApprovalDenied) {
			return InstallResult{Kind: InstallResultApprovalDenied}, nil
		}
		return InstallResult{}, err
	}
	current := result.Current()
	f.mu.Lock()
	stillLive := f.runtime == active && active != nil
	f.mu.Unlock()
	var pipeline *HTTPSPipelineDetail
	if stillLive {
		pipeline = active.engine.AdoptInstalledUserCA(current)
		f.mu.Lock()
		stillLive = f.runtime == active
		f.mu.Unlock()
		if stillLive && pipeline != nil && pipeline.Readiness == HTTPSReadinessReady {
			f.scheduleHTTPSDeadline(active, current)
		} else if stillLive {
			f.cancelHTTPSDeadline(active)
		}
	}
	f.mu.Lock()
	f.userCASnapshot = current.Snapshot()
	f.userCAAssessmentErr = nil
	f.userCACleanupIssue = nil
	f.mu.Unlock()
	kind := InstallResultAlreadyUsable
	if result.Changed() {
		kind = InstallResultInstalled
	}
	return InstallResult{
		Kind:               kind,
		InstalledCAExpires: current.Snapshot().ExpiresAt(),
		HTTPSPipeline:      pipeline,
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
		return UninstallResult{Kind: UninstallResultAlreadyMutating}, nil
	}
	f.mu.Lock()
	if f.ownerEnding || f.startMutating {
		ending := f.ownerEnding
		f.mu.Unlock()
		f.caAdmissionMu.Unlock()
		if ending {
			return UninstallResult{Kind: UninstallResultOwnerEnding}, nil
		}
		return UninstallResult{Kind: UninstallResultAlreadyMutating}, nil
	}
	active := f.runtime
	if active != nil && active.engine.interceptionActive() {
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
	defer f.finishCAMutation(active)
	if active != nil {
		active.engine.DeactivateHTTPS(userca.Snapshot{}, nil)
		f.cancelHTTPSDeadline(active)
	}
	result, err := f.userCA.Uninstall(context.Background())
	if err != nil {
		cleanupIssue := &UserCACleanupIssue{
			Cause:  err.Error(),
			Action: "Run `seamless-cors uninstall` again.",
		}
		f.mu.Lock()
		f.userCASnapshot = userca.Snapshot{}
		f.userCAAssessmentErr = err
		f.userCACleanupIssue = cleanupIssue
		f.mu.Unlock()
		return UninstallResult{
			Kind:         UninstallResultIncomplete,
			CleanupIssue: cleanupIssue,
		}, nil
	}
	current := result.Current()
	f.mu.Lock()
	f.userCASnapshot = current.Snapshot()
	f.userCAAssessmentErr = nil
	f.userCACleanupIssue = nil
	f.mu.Unlock()
	if !result.Changed() {
		return UninstallResult{Kind: UninstallResultAlreadyAbsent}, nil
	}
	return UninstallResult{Kind: UninstallResultUninstalled}, nil
}

func (f *lifecycle) uninstallConsentFingerprint(active *activeRuntime) string {
	state := active.engine.snapshot()
	sum := sha256.Sum256([]byte(state.ProxyListen + "\x00" + state.PACListen + "\x00uninstall-all-usercas"))
	return hex.EncodeToString(sum[:])
}

// watchRuntimeChanges consumes the runtime's coalesced status invalidations and
// complete desired PAC snapshots. Desired states are latest-value snapshots;
// status notifications only prompt consumers to read the current immutable
// runtime state.
func (f *lifecycle) watchRuntimeChanges(ctx context.Context, active *activeRuntime, baseline runtimeState) {
	lastPipelineRevision := baseline.HTTPSPipelineRevision
	for {
		select {
		case <-ctx.Done():
			return
		case projection := <-active.engine.PACProjections():
			f.managedPAC.PublishProjection(projection)
		case result := <-f.managedPAC.ReconciliationResults():
			f.mu.Lock()
			if f.runtime == active && active.managedPAC != nil {
				active.managedPAC.state = result.State()
				active.managedPAC.warnings = managedPACWarningDetails(result.Warnings())
			}
			f.mu.Unlock()
		case kind := <-active.engine.RuntimeChanges():
			state := active.engine.snapshot()
			consumePipeline := func() {
				if state.HTTPSPipelineRevision == lastPipelineRevision {
					return
				}
				lastPipelineRevision = state.HTTPSPipelineRevision
				f.mu.Lock()
				publish := f.httpsPipelineChanged
				stillActive := f.runtime == active
				if state.HTTPSPipeline == nil && active.assessmentCancel != nil {
					active.assessmentCancel()
				}
				f.mu.Unlock()
				if publish != nil && stillActive {
					publish(state.HTTPSPipeline)
				}
			}
			switch kind {
			case HTTPSPipelineChanged:
				consumePipeline()
				if state.HTTPSPipeline == nil || state.HTTPSPipeline.Readiness != HTTPSReadinessReady {
					f.cancelHTTPSDeadline(active)
				}
			case HTTPSAssessmentRequested:
				consumePipeline()
				if generation, ok := active.engine.pendingHTTPSAssessment(); ok {
					f.requestHTTPSAssessment(active, generation)
				}
			case RuntimeStatusChanged:
				// Status consumers read the complete Gateway snapshot. No
				// revision is needed to distinguish a source diagnostic. The
				// pipeline check also preserves an HTTPS invalidation
				// that was coalesced by this generic status notification.
				consumePipeline()
				if state.HTTPSPipeline == nil || state.HTTPSPipeline.Readiness != HTTPSReadinessReady {
					f.cancelHTTPSDeadline(active)
				}
			case HTTPSDeadlineReached:
				consumePipeline()
				if !active.engine.interceptionActive() {
					f.cancelHTTPSDeadline(active)
					continue
				}
				f.handleHTTPSDeadline(active, state.HTTPSGeneration)
			default:
				// Unknown kinds are ignored; the scoped vocabulary is private
				// to Gateway Runtime and this keeps a malformed notification
				// from stopping lifecycle orchestration.
			}
		}
	}
}

func (f *lifecycle) managedPACConsentDetail(snapshot managedpac.Snapshot) *ManagedPACConsentDetail {
	services := snapshot.Services()
	out := make([]ManagedPACServiceState, 0, len(services))
	for _, state := range services {
		out = append(out, ManagedPACServiceState{
			ServiceName: state.Name,
			Enabled:     state.Enabled,
			URL:         state.URL,
			Ownership:   pacOwnership(state.Ownership),
			Manageable:  state.Manageable(),
		})
	}
	proposed := snapshot.ManageableServices()
	return &ManagedPACConsentDetail{
		CurrentPACState:  out,
		ProposedServices: proposed,
		CleanupMode:      CleanupModeNoPACRestoration,
		Fingerprint:      pacConsentFingerprint(proposed),
	}
}

func pacConsentFingerprint(serviceNames []string) PACConsentFingerprint {
	h := sha256.New()
	for _, serviceName := range sortedUniqueServiceNames(serviceNames) {
		_, _ = h.Write([]byte(serviceName))
		_, _ = h.Write([]byte{0})
	}
	return PACConsentFingerprint(hex.EncodeToString(h.Sum(nil)))
}

func managedPACWarningDetails(warnings []managedpac.Warning) []ManagedPACWarningDetail {
	out := make([]ManagedPACWarningDetail, 0, len(warnings))
	for _, warning := range warnings {
		kind := ManagedPACWarningUpdateFailed
		if warning.Kind == managedpac.WarningDrift {
			kind = ManagedPACWarningDrift
		}
		out = append(out, ManagedPACWarningDetail{Kind: kind, ServiceName: warning.ServiceName, Diagnostic: warning.Diagnostic})
	}
	return out
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
	return inspectGatewayFootprint(ctx, f.managedPAC, f.coord, stale, runtimeActive, ownerCache)
}

func installedCAStatus(snapshot userca.Snapshot, assessmentErr error, mutating bool, cleanupIssue *UserCACleanupIssue) InstalledCAStatusDetail {
	if mutating {
		return InstalledCAStatusDetail{Health: CAHealthMutating, CleanupIssue: cleanupIssue}
	}
	if assessmentErr != nil || !snapshot.Usable() {
		return InstalledCAStatusDetail{Health: CAHealthNotUsable, CleanupIssue: cleanupIssue}
	}
	return InstalledCAStatusDetail{
		Health:       CAHealthUsable,
		Expires:      snapshot.ExpiresAt(),
		RenewalDue:   snapshot.RenewalDue(),
		CleanupIssue: cleanupIssue,
	}
}

package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/systempac"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
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
	StartResultStartAlreadyMutating                StartKind = "start-already-mutating"
	StartResultStopCancelled                       StartKind = "stop-cancelled"
	StartResultCleanupFailed                       StartKind = "cleanup-failed"
)

type StartRequest struct {
	WorkingDirectory            string                            `json:"workingDirectory" minLength:"1"`
	UpstreamListCreationConsent *UpstreamListCreationConsentInput `json:"upstreamListCreationConsent,omitempty"`
}

// StartResult is the closed semantic result of a Start operation.  Concrete
// variants carry only the payload that is legal for that outcome.
type StartResult interface {
	Kind() StartKind
	Fulfillment() CommandFulfillment
	UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail
	startResult()
}

// Started reports that the runtime was started and includes initial guidance.
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

// StartAlreadyMutating reports that another start/CA mutation is in progress.
type StartAlreadyMutating struct {
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

// StartStopCancelled reports that Stop cancelled the Start operation.
type StartStopCancelled struct {
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

type StartCleanupFailed struct {
	Failures                    []CleanupFailure
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
func (StartAlreadyMutating) Kind() StartKind                  { return StartResultStartAlreadyMutating }
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

type SystemPACServiceState struct {
	Name      string             `json:"name"`
	URL       string             `json:"url"`
	Enabled   bool               `json:"enabled"`
	Ownership SystemPACOwnership `json:"ownership"`
}

type SystemPACOwnership string

const (
	SystemPACOwnershipUnknown SystemPACOwnership = "unknown"
	SystemPACOwnershipEmpty   SystemPACOwnership = "empty"
	SystemPACOwnershipOwned   SystemPACOwnership = "owned"
	SystemPACOwnershipForeign SystemPACOwnership = "foreign"
)

type SystemPACIssueKind string

const (
	SystemPACIssueDiscovery    SystemPACIssueKind = "discovery"
	SystemPACIssueObservation  SystemPACIssueKind = "observation"
	SystemPACIssueMutation     SystemPACIssueKind = "mutation"
	SystemPACIssueVerification SystemPACIssueKind = "verification"
	SystemPACIssueResidue      SystemPACIssueKind = "residue"
)

type SystemPACIssue struct {
	Kind        SystemPACIssueKind `json:"kind"`
	ServiceName string             `json:"serviceName,omitempty"`
	Cause       string             `json:"cause"`
}

type SystemPACReport struct {
	Generation            uint64                  `json:"generation,omitempty"`
	Services              []SystemPACServiceState `json:"services"`
	RoutesCurrentEndpoint bool                    `json:"routesCurrentEndpoint"`
	Issues                []SystemPACIssue        `json:"issues,omitempty"`
}

type StartGuidance struct {
	UpstreamLists []UpstreamListSourceDetail `json:"upstreamLists"`
	SystemPAC     SystemPACReport            `json:"systemPac"`
	Traffic       TrafficStatusDetail        `json:"traffic"`
	InstalledCA   InstalledCAStatusDetail    `json:"installedCA"`
	UserCAIssue   *UserCAAssessmentIssue     `json:"userCAAssessmentIssue,omitempty"`
}

type UpstreamListSourceKind string

const (
	UpstreamListSourceGlobal    UpstreamListSourceKind = "global"
	UpstreamListSourceDirectory UpstreamListSourceKind = "directory"
)

type UpstreamListSourceDetail struct {
	Kind            UpstreamListSourceKind       `json:"kind"`
	Path            string                       `json:"path"`
	Warnings        []UpstreamListWarningDetail  `json:"warnings,omitempty"`
	FileSyncIssue   *FileSyncIssue               `json:"fileSyncIssue,omitempty"`
	ProjectionIssue *UpstreamListProjectionIssue `json:"projectionIssue,omitempty"`
}

type UpstreamListWarningDetail struct {
	Source     UpstreamListSourceKind `json:"source"`
	Path       string                 `json:"path"`
	Line       int                    `json:"line"`
	Text       string                 `json:"text"`
	Diagnostic string                 `json:"diagnostic"`
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
	StopResultStopped    StopResultKind = "stopped"
	StopResultNotRunning StopResultKind = "not-running"
)

type StopResult struct {
	Kind               StopResultKind
	Warnings           []CommandWarning
	CleanupFulfillment CommandFulfillment
	SystemPACCleanup   SystemPACReport
	CleanupFailures    []CleanupFailure
}

func (r StopResult) Fulfillment() CommandFulfillment {
	return CommandFulfilled
}

type CleanupFailure struct {
	Subject    CleanupSubjectKind `json:"subject"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

type CleanupSubjectKind string

const (
	CleanupSubjectSystemPAC         CleanupSubjectKind = "system-pac"
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
	InstallResultAlreadyMutating InstallResultKind = "already-mutating"
	InstallResultOwnerEnding     InstallResultKind = "owner-ending"
	InstallResultOwnerTransition InstallResultKind = "owner-transition"
)

type InstallResult struct {
	Kind               InstallResultKind
	InstalledCAExpires time.Time
}

func (r InstallResult) Fulfillment() CommandFulfillment {
	if r.Kind == InstallResultInstalled {
		return CommandFulfilled
	}
	return CommandUnfulfilled
}

type UninstallResultKind string

const (
	UninstallResultUninstalled     UninstallResultKind = "uninstalled"
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
	if r.Kind == UninstallResultUninstalled {
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
	State                 GatewayStatusKind       `json:"state"`
	Owner                 *OwnerStatusDetail      `json:"owner,omitempty"`
	Runtime               *RuntimeStatusDetail    `json:"runtime,omitempty"`
	SystemPAC             SystemPACReport         `json:"systemPac"`
	Cleanup               CleanupStatusDetail     `json:"cleanup"`
	InstalledCA           InstalledCAStatusDetail `json:"installedCA"`
	UserCAAssessmentIssue *UserCAAssessmentIssue  `json:"userCAAssessmentIssue,omitempty"`
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
	ProxyListen             string                     `json:"proxyListen"`
	PACListen               string                     `json:"pacListen"`
	UpstreamLists           []UpstreamListSourceDetail `json:"upstreamLists"`
	UpstreamCount           int                        `json:"upstreamCount"`
	Traffic                 TrafficStatusDetail        `json:"traffic"`
	LatestSystemPACDelivery *SystemPACReport           `json:"latestSystemPacDelivery,omitempty"`
}

type TrafficFeatureState string

const (
	TrafficFeatureActive   TrafficFeatureState = "active"
	TrafficFeatureBlocked  TrafficFeatureState = "blocked"
	TrafficFeatureInactive TrafficFeatureState = "inactive"
)

type TrafficStatusDetail struct {
	RoutingReady      bool                `json:"routingReady"`
	ProjectionCurrent bool                `json:"projectionCurrent"`
	HTTPCORS          TrafficFeatureState `json:"httpCors"`
	HTTPSCORS         TrafficFeatureState `json:"httpsCors"`
	HTTPSFacade       TrafficFeatureState `json:"httpsFacade"`
}

type UserCAAssessmentIssue struct {
	Cause  string `json:"cause"`
	Action string `json:"action,omitempty"`
}

type UserCACleanupIssue struct {
	Cause  string `json:"cause"`
	Action string `json:"action,omitempty"`
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
	mu                     sync.Mutex
	deliveryMu             sync.Mutex
	caAdmissionMu          sync.Mutex
	systemPAC              systempac.Module
	userCA                 userCAModule
	userCAState            userCAState
	userCAAssessmentErr    error
	coord                  *coordinator
	runtimeDir             string
	globalUpstreamListPath string
	routerListen           string
	ownerCache             stateCache
	startMutating          bool
	startCancel            context.CancelFunc
	startDone              chan struct{}
	ownerEnding            bool
	transientOwner         bool
	caMutating             bool
	assessmentPending      bool
	runtime                *activeRuntime
	userCACleanupIssue     *UserCACleanupIssue
	fatal                  chan error
}

type activeRuntime struct {
	engine             *trafficRuntime
	latestPACDelivery  *SystemPACReport
	ctx                context.Context
	cancel             context.CancelFunc
	done               chan error
	phase              runtimePhase
	deadlineTimer      *time.Timer
	deadlineGeneration uint64
}

type runtimePhase string

const (
	runtimePhaseStarting runtimePhase = "starting"
	runtimePhaseRunning  runtimePhase = "running"
)

func newLifecycle(pac systempac.Module, ca userCAModule, coord *coordinator, routerListen string) (*lifecycle, error) {
	return newLifecycleState(pac, ca, coord, routerListen, true)
}

func newLifecycleUninspected(pac systempac.Module, ca userCAModule, coord *coordinator, routerListen string) (*lifecycle, error) {
	return newLifecycleState(pac, ca, coord, routerListen, false)
}

func newLifecycleState(
	pac systempac.Module,
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
		pac = openSystemPAC()
	}
	var initial userCAState
	var assessmentErr error
	if inspectUserCA {
		initial, assessmentErr = ca.Inspect(context.Background())
	}
	return &lifecycle{
		systemPAC:              pac,
		userCA:                 ca,
		userCAState:            initial,
		userCAAssessmentErr:    assessmentErr,
		coord:                  coord,
		runtimeDir:             coord.RuntimeDirPath(),
		globalUpstreamListPath: defaultGlobalUpstreamListPath(),
		routerListen:           routerListen,
		fatal:                  make(chan error, 1),
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

func (f *lifecycle) scheduleUserCADeadline(active *activeRuntime, current userCAState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runtime != active {
		return
	}
	f.scheduleUserCADeadlineLocked(active, current)
}

func (f *lifecycle) scheduleUserCADeadlineLocked(active *activeRuntime, current userCAState) {
	if !current.Usable {
		return
	}
	state := active.engine.snapshot()
	if active.deadlineTimer != nil {
		active.deadlineTimer.Stop()
	}
	active.deadlineGeneration = state.UserCARevision
	now := time.Now()
	delay := current.ExpiresAt.Sub(now)
	if delay < 0 {
		delay = 0
	}
	generation := active.deadlineGeneration
	active.deadlineTimer = time.AfterFunc(delay, func() {
		f.handleUserCADeadline(active, generation)
	})
}

func (f *lifecycle) cancelUserCADeadline(active *activeRuntime) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if active.deadlineTimer != nil {
		active.deadlineTimer.Stop()
		active.deadlineTimer = nil
	}
	active.deadlineGeneration = 0
}

func (f *lifecycle) handleUserCADeadline(active *activeRuntime, revision uint64) {
	if !f.caAdmissionMu.TryLock() {
		f.mu.Lock()
		if f.runtime == active {
			f.assessmentPending = true
		}
		f.mu.Unlock()
		return
	}
	defer f.finishUserCAAssessment(active)
	f.mu.Lock()
	if f.runtime != active {
		f.mu.Unlock()
		return
	}
	if f.caMutating || f.startMutating {
		f.assessmentPending = true
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()
	if !active.engine.ExpireUserCA(revision) {
		return
	}
	f.mu.Lock()
	if f.runtime == active {
		f.userCAState = userCAState{}
		f.userCAAssessmentErr = nil
	}
	f.mu.Unlock()
	f.assessUserCA(active)
}

func (f *lifecycle) requestUserCAAssessment(active *activeRuntime) {
	if !f.caAdmissionMu.TryLock() {
		f.mu.Lock()
		if f.runtime == active {
			f.assessmentPending = true
		}
		f.mu.Unlock()
		return
	}
	go func() {
		defer f.finishUserCAAssessment(active)
		f.assessUserCA(active)
	}()
}

func (f *lifecycle) finishUserCAAssessment(active *activeRuntime) {
	f.caAdmissionMu.Unlock()
	f.mu.Lock()
	pending := f.assessmentPending && f.runtime == active
	f.assessmentPending = false
	f.mu.Unlock()
	if pending {
		f.requestUserCAAssessment(active)
	}
}

func (f *lifecycle) assessUserCA(active *activeRuntime) {
	f.mu.Lock()
	if f.runtime != active {
		f.mu.Unlock()
		return
	}
	ctx := active.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	assessmentCtx, cancel := context.WithCancel(ctx)
	f.mu.Unlock()
	defer cancel()

	assessment, assessmentErr := f.userCA.Inspect(assessmentCtx)

	f.mu.Lock()
	stillActive := f.runtime == active
	if stillActive {
		f.userCAState = assessment
		f.userCAAssessmentErr = assessmentErr
	}
	f.mu.Unlock()
	if !stillActive {
		return
	}
	active.engine.AdoptUserCA(assessment, assessmentErr)
	if assessmentErr == nil && assessment.Usable {
		f.scheduleUserCADeadline(active, assessment)
	} else {
		f.cancelUserCADeadline(active)
	}
}

func (f *lifecycle) finishCAMutation(active *activeRuntime) {
	f.mu.Lock()
	if f.transientOwner {
		f.ownerEnding = true
	}
	f.caMutating = false
	pending := f.assessmentPending && f.runtime == active && active != nil
	f.assessmentPending = false
	f.mu.Unlock()
	f.caAdmissionMu.Unlock()
	if pending {
		f.requestUserCAAssessment(active)
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
		active := f.runtime
		f.mu.Unlock()
		if _, delivered := f.deliverSystemPAC(ctx, active); !delivered {
			return StartStopCancelled{}, nil
		}
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
		pending := f.assessmentPending && active != nil
		f.assessmentPending = false
		f.mu.Unlock()
		if pending {
			f.requestUserCAAssessment(active)
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
	ownerCache := f.ownerCache
	f.mu.Unlock()
	if startCancel != nil {
		startCancel()
	}
	if active != nil {
		f.cancelUserCADeadline(active)
	}
	var cleanupFailures []CleanupFailure
	if startDone != nil {
		<-startDone
	}
	f.deliveryMu.Lock()
	cleanupServices, cleanupErr := f.systemPAC.Cleanup(ctx)
	if cleanupErr != nil {
		cleanupFailures = append(cleanupFailures, CleanupFailure{Subject: CleanupSubjectSystemPAC, Diagnostic: cleanupErr.Error()})
	}
	f.deliveryMu.Unlock()
	if active != nil {
		active.cancel()
		if err := active.engine.CloseTraffic(); err != nil {
			warnings = append(warnings, CommandWarning{Kind: CommandWarningRuntimeCloseFailed, Diagnostic: err.Error()})
		}
	}
	f.mu.Lock()
	if f.runtime == active {
		f.runtime = nil
	}
	f.mu.Unlock()
	// Once traffic and any preempted Start have settled, wait for admitted
	// owner-owned CA work before durable footprint cleanup.
	f.caAdmissionMu.Lock()
	f.caAdmissionMu.Unlock()
	var ownedCache *stateCache
	if ownerCache.HTTPRouterListen != "" && ownerCache.Token != "" {
		ownedCache = &ownerCache
	}
	cleanupFailures = append(cleanupFailures, cleanGatewayStateCache(f.coord, ownedCache)...)
	cleanupFulfillment := CommandFulfilled
	if len(cleanupFailures) > 0 {
		cleanupFulfillment = CommandUnfulfilled
	}
	return StopResult{Kind: StopResultStopped, Warnings: warnings, CleanupFulfillment: cleanupFulfillment, SystemPACCleanup: cleanupSystemPACReport(cleanupServices, cleanupErr), CleanupFailures: cleanupFailures}, nil
}

func (f *lifecycle) Status(ctx context.Context, stale bool) (StatusResult, error) {
	f.mu.Lock()
	active := f.runtime
	ownerCache := f.ownerCache
	ownerEnding := f.ownerEnding
	caState := f.userCAState
	caAssessmentErr := f.userCAAssessmentErr
	caMutating := f.caMutating
	caCleanupIssue := f.userCACleanupIssue
	var phase runtimePhase
	var latestPACDelivery *SystemPACReport
	if active != nil {
		phase = active.phase
		latestPACDelivery = active.latestPACDelivery
	}
	f.mu.Unlock()
	var endpoint string
	if active != nil {
		endpoint = active.engine.PACListen()
	}
	pacState, pacErr := f.systemPAC.Observe(ctx, endpoint)
	currentPAC := systemPACReport(pacState, pacErr)
	result := StatusResult{
		Kind: StatusResultReported,
		StatusReport: StatusReport{
			State:                 GatewayStatusNotRunning,
			SystemPAC:             currentPAC,
			Cleanup:               f.cleanupStatus(stale, ownerCache, currentPAC),
			InstalledCA:           installedCAStatus(caState, caAssessmentErr, caMutating, caCleanupIssue),
			UserCAAssessmentIssue: userCAAssessmentIssue(caAssessmentErr),
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
			ProxyListen:             state.ProxyListen,
			PACListen:               state.PACListen,
			UpstreamLists:           state.UpstreamLists,
			UpstreamCount:           state.UpstreamCount,
			Traffic:                 trafficStatus(state, currentPAC.RoutesCurrentEndpoint),
			LatestSystemPACDelivery: latestPACDelivery,
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

func upstreamListWarningDetails(kind UpstreamListSourceKind, path string, warnings []upstreamlist.Warning) []UpstreamListWarningDetail {
	details := make([]UpstreamListWarningDetail, 0, len(warnings))
	for _, warning := range warnings {
		details = append(details, UpstreamListWarningDetail{
			Source:     kind,
			Path:       path,
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
	// Withdraw UserCA-backed routes before trust or signing material changes.
	if active != nil {
		active.engine.AdoptUserCA(userCAState{}, nil)
		f.cancelUserCADeadline(active)
	}
	// Once admitted, CA work belongs to the owner rather than the request.
	current, err := f.userCA.Install(context.Background())
	if err != nil {
		if active != nil {
			active.engine.AdoptUserCA(userCAState{}, err)
		}
		f.mu.Lock()
		f.userCAState = userCAState{}
		f.userCAAssessmentErr = err
		f.mu.Unlock()
		return InstallResult{}, err
	}
	f.mu.Lock()
	stillLive := f.runtime == active && active != nil
	f.mu.Unlock()
	if stillLive {
		active.engine.AdoptUserCA(current, nil)
		f.mu.Lock()
		stillLive = f.runtime == active
		f.mu.Unlock()
		if stillLive {
			f.scheduleUserCADeadline(active, current)
		}
	}
	f.mu.Lock()
	f.userCAState = current
	f.userCAAssessmentErr = nil
	f.userCACleanupIssue = nil
	f.mu.Unlock()
	return InstallResult{
		Kind:               InstallResultInstalled,
		InstalledCAExpires: current.ExpiresAt,
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
		active.engine.AdoptUserCA(userCAState{}, nil)
		f.cancelUserCADeadline(active)
	}
	err := f.userCA.Uninstall(context.Background())
	if err != nil {
		if active != nil {
			active.engine.AdoptUserCA(userCAState{}, err)
		}
		cleanupIssue := &UserCACleanupIssue{
			Cause:  err.Error(),
			Action: "Run `seamless-cors uninstall` again.",
		}
		f.mu.Lock()
		f.userCAState = userCAState{}
		f.userCAAssessmentErr = err
		f.userCACleanupIssue = cleanupIssue
		f.mu.Unlock()
		return UninstallResult{
			Kind:         UninstallResultIncomplete,
			CleanupIssue: cleanupIssue,
		}, nil
	}
	f.mu.Lock()
	f.userCAState = userCAState{}
	f.userCAAssessmentErr = nil
	f.userCACleanupIssue = nil
	f.mu.Unlock()
	return UninstallResult{Kind: UninstallResultUninstalled}, nil
}

func (f *lifecycle) uninstallConsentFingerprint(active *activeRuntime) string {
	state := active.engine.snapshot()
	sum := sha256.Sum256([]byte(state.ProxyListen + "\x00" + state.PACListen + "\x00uninstall-all-usercas"))
	return hex.EncodeToString(sum[:])
}

// watchRuntimeChanges coordinates independent runtime requests.
func (f *lifecycle) watchRuntimeChanges(ctx context.Context, active *activeRuntime) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-active.engine.DeliveryRequests():
			_, _ = f.deliverSystemPAC(ctx, active)
		case <-active.engine.UserCAAssessmentRequests():
			f.requestUserCAAssessment(active)
		}
	}
}

func (f *lifecycle) deliverSystemPAC(ctx context.Context, active *activeRuntime) (SystemPACReport, bool) {
	f.deliveryMu.Lock()
	defer f.deliveryMu.Unlock()
	f.mu.Lock()
	admitted := !f.ownerEnding && f.runtime == active
	f.mu.Unlock()
	if !admitted {
		return SystemPACReport{}, false
	}
	state, err := f.systemPAC.Deliver(ctx, active.engine.PACListen())
	report := systemPACReport(state, err)
	f.mu.Lock()
	if f.runtime == active && !f.ownerEnding {
		active.latestPACDelivery = &report
	}
	f.mu.Unlock()
	return report, true
}

func (f *lifecycle) cleanupStatus(stale bool, ownerCache stateCache, pac SystemPACReport) CleanupStatusDetail {
	return inspectGatewayFootprint(f.coord, stale, ownerCache, pac)
}

func trafficStatus(state runtimeState, routingReady bool) TrafficStatusDetail {
	httpActive := routingReady && state.ServedHTTPCORS
	httpsActive := routingReady && state.ServedHTTPSCORS && state.UserCAUsable && state.UserCAIdentityMatches
	facadeActive := routingReady && state.ServedHTTPSFacade && state.UserCAUsable && state.UserCAIdentityMatches
	return TrafficStatusDetail{
		RoutingReady:      routingReady,
		ProjectionCurrent: state.TrafficProjectionCurrent,
		HTTPCORS:          featureState(httpActive, state.HTTPDemand),
		HTTPSCORS:         featureState(httpsActive, state.HTTPSDemand),
		HTTPSFacade:       featureState(facadeActive, false),
	}
}

func featureState(active, demanded bool) TrafficFeatureState {
	if active {
		return TrafficFeatureActive
	}
	if demanded {
		return TrafficFeatureBlocked
	}
	return TrafficFeatureInactive
}

func installedCAStatus(current userCAState, assessmentErr error, mutating bool, cleanupIssue *UserCACleanupIssue) InstalledCAStatusDetail {
	if mutating {
		return InstalledCAStatusDetail{Health: CAHealthMutating, CleanupIssue: cleanupIssue}
	}
	if assessmentErr != nil || !current.Usable {
		return InstalledCAStatusDetail{Health: CAHealthNotUsable, CleanupIssue: cleanupIssue}
	}
	return InstalledCAStatusDetail{
		Health:       CAHealthUsable,
		Expires:      current.ExpiresAt,
		RenewalDue:   current.RenewalDue,
		CleanupIssue: cleanupIssue,
	}
}

func userCAAssessmentIssue(err error) *UserCAAssessmentIssue {
	if err == nil {
		return nil
	}
	return &UserCAAssessmentIssue{Cause: err.Error()}
}

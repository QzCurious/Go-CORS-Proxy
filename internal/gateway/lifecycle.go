package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"seamless-cors/internal/liveconfig"
	"seamless-cors/internal/managedpac"
	"seamless-cors/internal/userca"
)

type StartResultKind string

const (
	StartResultStarted                StartResultKind = "started"
	StartResultAlreadyRunning         StartResultKind = "already-running"
	StartResultOwnerAlreadyRunning    StartResultKind = "owner-already-running"
	StartResultConsentRequired        StartResultKind = "consent-required"
	StartResultPACReplacementDeclined StartResultKind = "pac-replacement-declined"
	StartResultStartAlreadyMutating   StartResultKind = "start-already-mutating"
	StartResultPlatformApprovalDenied StartResultKind = "platform-approval-denied"
	StartResultStopCancelled          StartResultKind = "stop-cancelled"
	StartResultCleanupFailed          StartResultKind = "cleanup-failed"
)

// StartError preserves a completed CA Ensure result when a later activation
// step fails. Command transports must carry both pieces of information.
type StartError struct {
	Diagnostic string          `json:"diagnostic"`
	CAEnsure   *CAEnsureResult `json:"caEnsure,omitempty"`
	Cause      error           `json:"-"`
}

func (e *StartError) Error() string { return e.Diagnostic }
func (e *StartError) Unwrap() error { return e.Cause }

type StartRequest struct {
	PACReplacementConsent *PACReplacementConsentInput `json:"pacReplacementConsent,omitempty"`
}

type StartResult struct {
	Kind                  StartResultKind              `json:"kind"`
	PACReplacementConsent *PACReplacementConsentDetail `json:"pacReplacementConsent,omitempty"`
	CAEnsure              *CAEnsureResult              `json:"caEnsure,omitempty"`
	Guidance              *StartGuidanceDetail         `json:"guidance,omitempty"`
	CleanupFailures       []CleanupFailureDetail       `json:"cleanupFailures,omitempty"`
}

type CAEnsureResultKind string

const (
	CAEnsureResultInstalled     CAEnsureResultKind = "installed"
	CAEnsureResultAlreadyUsable CAEnsureResultKind = "already-usable"
)

type CAEnsureResult struct {
	Kind    CAEnsureResultKind `json:"kind"`
	Expires time.Time          `json:"expires"`
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
	ConfigPath         string   `json:"configPath"`
	DomainListPath     string   `json:"domainListPath"`
	ManagedPACActive   bool     `json:"managedPacActive"`
	ManagedPACServices []string `json:"managedPacServices,omitempty"`
	CATrusted          bool     `json:"caTrusted"`
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

const CommandWarningRuntimeCloseFailed CommandWarningKind = "runtime-close-failed"

type InstallResultKind string

const (
	InstallResultInstalled            InstallResultKind = "installed"
	InstallResultAlreadyUsable        InstallResultKind = "already-usable"
	InstallResultBlockedRuntimeActive InstallResultKind = "blocked-runtime-active"
)

type InstallResult struct {
	Kind               InstallResultKind `json:"kind"`
	InstalledCAExpires time.Time         `json:"installedCAExpires,omitempty"`
	Advisories         []InstallAdvisory `json:"advisories,omitempty"`
}

type InstallAdvisory struct {
	Kind                    InstallAdvisoryKind              `json:"kind"`
	ConfigCATrustedDisabled *ConfigCATrustedDisabledAdvisory `json:"configCaTrustedDisabled,omitempty"`
}

type InstallAdvisoryKind string

const InstallAdvisoryConfigCATrustedDisabled InstallAdvisoryKind = "config-ca-trusted-disabled"

type ConfigCATrustedDisabledAdvisory struct {
	ConfigPath    string `json:"configPath"`
	Setting       string `json:"setting"`
	CurrentValue  bool   `json:"currentValue"`
	RequiredValue bool   `json:"requiredValue"`
}

type UninstallResultKind string

const (
	UninstallResultUninstalled          UninstallResultKind = "uninstalled"
	UninstallResultAlreadyAbsent        UninstallResultKind = "already-absent"
	UninstallResultBlockedRuntimeActive UninstallResultKind = "blocked-runtime-active"
)

type UninstallResult struct {
	Kind UninstallResultKind `json:"kind"`
}

type GatewayStatusKind string

const (
	GatewayStatusNotRunning GatewayStatusKind = "not-running"
	GatewayStatusStaleCache GatewayStatusKind = "stale-cache"
	GatewayStatusRouterOnly GatewayStatusKind = "router-only"
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
	ProxyListen        string                       `json:"proxyListen"`
	PACListen          string                       `json:"pacListen"`
	DomainListPath     string                       `json:"domainListPath"`
	DomainCount        int                          `json:"domainCount"`
	CATrusted          bool                         `json:"caTrusted"`
	ManagedPACServices []string                     `json:"managedPacServices,omitempty"`
	PendingLifecycle   []PendingLifecycleChangeKind `json:"pendingLifecycle,omitempty"`
}

type PendingLifecycleChangeKind string

const PendingLifecycleChangeCATrusted PendingLifecycleChangeKind = "ca-trusted"

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
	CAHealthUsable             CAHealthStatus = "usable"
	CAHealthMissing            CAHealthStatus = "missing"
	CAHealthExpired            CAHealthStatus = "expired"
	CAHealthExpiringSoon       CAHealthStatus = "expiring-soon"
	CAHealthInvalid            CAHealthStatus = "invalid"
	CAHealthMultiple           CAHealthStatus = "multiple"
	CAHealthMismatchedMaterial CAHealthStatus = "mismatched-material"
	CAHealthUnknown            CAHealthStatus = "unknown"
)

type lifecycle struct {
	mu                   sync.Mutex
	caAdmissionMu        sync.Mutex
	managedPACSettings   managedpac.SystemSettings
	userCATrustStore     userca.TrustStore
	coord                *coordinator
	runtimeDir           string
	routerListen         string
	ownerCache           stateCache
	startMutating        bool
	startCleanupComplete bool
	startCancel          context.CancelFunc
	startDone            chan struct{}
	runtime              *activeRuntime
	fatal                chan error
}

type activeRuntime struct {
	engine   *trafficRuntime
	snapshot liveconfig.Snapshot
	pac      *managedpac.Session
	cancel   context.CancelFunc
	done     chan error
	phase    runtimePhase
}

type runtimePhase string

const (
	runtimePhaseStarting runtimePhase = "starting"
	runtimePhaseRunning  runtimePhase = "running"
)

func newLifecycle(settings managedpac.SystemSettings, trustStore userca.TrustStore, coord *coordinator, routerListen string) (*lifecycle, error) {
	if coord == nil {
		var err error
		coord, err = defaultCoordinator()
		if err != nil {
			return nil, err
		}
	}
	return &lifecycle{
		managedPACSettings: settings,
		userCATrustStore:   trustStore,
		coord:              coord,
		runtimeDir:         coord.RuntimeDirPath(),
		routerListen:       routerListen,
		fatal:              make(chan error, 1),
	}, nil
}

func liveconfigLoadOrBootstrap() (*liveconfig.Source, liveconfig.Snapshot, error) {
	return liveconfig.LoadOrBootstrap("", nil)
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
	if f.runtime != nil {
		f.mu.Unlock()
		return StartResult{Kind: StartResultAlreadyRunning}, nil
	}
	if f.startMutating {
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
		InstalledCA: f.installedCAStatus(ctx),
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
			ProxyListen:        state.ProxyListen,
			PACListen:          state.PACListen,
			DomainListPath:     state.DomainList,
			DomainCount:        state.DomainCount,
			CATrusted:          state.CATrusted,
			ManagedPACServices: managedPACServices(pac),
			PendingLifecycle:   pendingLifecycleKinds(state.PendingLifecycle),
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

func (f *lifecycle) Install(ctx context.Context) (InstallResult, error) {
	f.caAdmissionMu.Lock()
	defer f.caAdmissionMu.Unlock()
	caDir, err := liveconfig.CADir()
	if err != nil {
		return InstallResult{}, err
	}
	if f.activeRuntimeCATrusted() {
		report, inspectErr := userca.InspectContext(ctx, caDir, f.userCATrustStore)
		if inspectErr != nil {
			return InstallResult{}, inspectErr
		}
		if report.Health == userca.HealthUsable {
			return InstallResult{
				Kind:               InstallResultAlreadyUsable,
				InstalledCAExpires: report.Expires,
				Advisories:         installAdvisories(),
			}, nil
		}
		return InstallResult{Kind: InstallResultBlockedRuntimeActive}, nil
	}
	_, result, err := userca.EnsureContext(ctx, caDir, f.userCATrustStore)
	if err != nil {
		return InstallResult{}, err
	}
	kind := InstallResultAlreadyUsable
	if result.Changed {
		kind = InstallResultInstalled
	}
	return InstallResult{
		Kind:               kind,
		InstalledCAExpires: result.Expires,
		Advisories:         installAdvisories(),
	}, nil
}

func (f *lifecycle) Uninstall(ctx context.Context) (UninstallResult, error) {
	f.caAdmissionMu.Lock()
	defer f.caAdmissionMu.Unlock()
	if f.activeRuntimeCATrusted() {
		return UninstallResult{Kind: UninstallResultBlockedRuntimeActive}, nil
	}
	caDir, err := liveconfig.CADir()
	if err != nil {
		return UninstallResult{}, err
	}
	before, _ := userca.InspectContext(ctx, caDir, f.userCATrustStore)
	if err := userca.UninstallContext(ctx, caDir, f.userCATrustStore); err != nil {
		return UninstallResult{}, err
	}
	after, err := userca.InspectContext(ctx, caDir, f.userCATrustStore)
	if err != nil {
		return UninstallResult{}, err
	}
	if after.Health != userca.HealthMissing {
		return UninstallResult{}, fmt.Errorf("Installed User CA uninstall incomplete: installed-ca: %s", after.Health)
	}
	if before.Health == userca.HealthMissing {
		return UninstallResult{Kind: UninstallResultAlreadyAbsent}, nil
	}
	return UninstallResult{Kind: UninstallResultUninstalled}, nil
}

func (f *lifecycle) activeRuntimeCATrusted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runtime != nil && f.runtime.snapshot.CATrusted()
}

func (f *lifecycle) watchPACRefreshes(ctx context.Context, active *activeRuntime) {
	for {
		select {
		case <-ctx.Done():
			return
		case nextURL := <-active.engine.PACURLUpdates():
			if err := active.pac.Refresh(ctx, nextURL); err != nil {
				f.reportFatalRuntimeError(active, err)
				return
			}
		}
	}
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

func (f *lifecycle) installedCAStatus(ctx context.Context) InstalledCAStatusDetail {
	caDir, err := liveconfig.CADir()
	if err != nil {
		return InstalledCAStatusDetail{Health: CAHealthUnknown}
	}
	report, err := userca.InspectContext(ctx, caDir, f.userCATrustStore)
	if err != nil {
		return InstalledCAStatusDetail{Health: CAHealthUnknown}
	}
	return InstalledCAStatusDetail{Health: caHealthStatus(report.Health), Expires: report.Expires}
}

func caHealthStatus(health userca.Health) CAHealthStatus {
	switch health {
	case userca.HealthUsable:
		return CAHealthUsable
	case userca.HealthMissing:
		return CAHealthMissing
	case userca.HealthExpired:
		return CAHealthExpired
	case userca.HealthExpiringSoon:
		return CAHealthExpiringSoon
	case userca.HealthInvalid:
		return CAHealthInvalid
	case userca.HealthMultiple:
		return CAHealthMultiple
	case userca.HealthMismatchedMaterial:
		return CAHealthMismatchedMaterial
	default:
		return CAHealthUnknown
	}
}

func pendingLifecycleKinds(values []string) []PendingLifecycleChangeKind {
	var kinds []PendingLifecycleChangeKind
	for _, value := range values {
		if value == string(PendingLifecycleChangeCATrusted) {
			kinds = append(kinds, PendingLifecycleChangeCATrusted)
		}
	}
	return kinds
}

func installAdvisories() []InstallAdvisory {
	loaded, err := liveconfig.LoadExisting("")
	if err != nil || loaded.CATrusted() {
		return nil
	}
	return []InstallAdvisory{{
		Kind: InstallAdvisoryConfigCATrustedDisabled,
		ConfigCATrustedDisabled: &ConfigCATrustedDisabledAdvisory{
			ConfigPath:    loaded.ConfigPath(),
			Setting:       "ca-trusted",
			CurrentValue:  false,
			RequiredValue: true,
		},
	}}
}

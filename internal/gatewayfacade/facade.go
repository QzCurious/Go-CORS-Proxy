package gatewayfacade

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"seamless-cors/internal/cleanup"
	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayruntime"
	"seamless-cors/internal/liveconfig"
	"seamless-cors/internal/managedpac"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

type StartPlanKind string

const (
	StartPlanReady           StartPlanKind = "ready"
	StartPlanConsentRequired StartPlanKind = "consent-required"
	StartPlanAlreadyRunning  StartPlanKind = "already-running"
)

type StartResultKind string

const (
	StartResultStarted                StartResultKind = "started"
	StartResultAlreadyRunning         StartResultKind = "already-running"
	StartResultConsentRequired        StartResultKind = "consent-required"
	StartResultStartAlreadyMutating   StartResultKind = "start-already-mutating"
	StartResultPlatformApprovalDenied StartResultKind = "platform-approval-denied"
)

type StartPlan struct {
	Kind                  StartPlanKind                `json:"kind"`
	PACReplacementConsent *PACReplacementConsentDetail `json:"pacReplacementConsent,omitempty"`
}

type StartRequest struct {
	PACReplacementConsent *PACReplacementConsentInput `json:"pacReplacementConsent,omitempty"`
}

type StartResult struct {
	Kind                  StartResultKind              `json:"kind"`
	PACReplacementConsent *PACReplacementConsentDetail `json:"pacReplacementConsent,omitempty"`
	Guidance              *StartGuidanceDetail         `json:"guidance,omitempty"`
}

type PACReplacementConsentDetail struct {
	CurrentPACState []ManagedPACServiceState `json:"currentPacState"`
	CleanupMode     CleanupMode              `json:"cleanupMode"`
}

type CleanupMode string

const CleanupModeNoPACRestoration CleanupMode = "no-pac-restoration"

type ManagedPACServiceState struct {
	ServiceName string       `json:"serviceName"`
	Enabled     bool         `json:"enabled"`
	URL         string       `json:"url"`
	Ownership   PACOwnership `json:"ownership"`
}

type PACOwnership string

const (
	PACOwnershipEmpty   PACOwnership = "empty"
	PACOwnershipOwned   PACOwnership = "owned"
	PACOwnershipForeign PACOwnership = "foreign"
)

type PACReplacementConsentInput struct {
	Accepted bool `json:"accepted"`
}

type StartGuidanceDetail struct {
	ConfigPath         string   `json:"configPath"`
	DomainListPath     string   `json:"domainListPath"`
	ManagedPACActive   bool     `json:"managedPacActive"`
	ManagedPACServices []string `json:"managedPacServices,omitempty"`
	CATrusted          bool     `json:"caTrusted"`
	InstalledCAChanged bool     `json:"installedCaChanged,omitempty"`
}

type StopResultKind string

const (
	StopResultStopped       StopResultKind = "stopped"
	StopResultCleanupFailed StopResultKind = "cleanup-failed"
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
	InstallResultInstalled     InstallResultKind = "installed"
	InstallResultAlreadyUsable InstallResultKind = "already-usable"
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
	Needed   bool                 `json:"needed"`
	Subjects []CleanupSubjectKind `json:"subjects,omitempty"`
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
	CAHealthUnsupported        CAHealthStatus = "unsupported"
	CAHealthUnknown            CAHealthStatus = "unknown"
)

type CheckResult struct {
	Platform          string                    `json:"platform"`
	Supported         bool                      `json:"supported"`
	PACManagement     platform.CapabilityStatus `json:"pacManagement"`
	CATrustManagement platform.CapabilityStatus `json:"caTrustManagement"`
	RuntimeCleanup    platform.CapabilityStatus `json:"runtimeCleanup"`
}

type Facade struct {
	mu            sync.Mutex
	adapter       platform.Adapter
	coord         *gatewaycoord.Coordinator
	runtimeDir    string
	routerListen  string
	ownerCache    gatewaycoord.GatewayStateCache
	startMutating bool
	runtime       *activeRuntime
	fatal         chan error
}

type activeRuntime struct {
	engine *gatewayruntime.Runtime
	live   liveconfig.Config
	pac    *managedpac.Session
	cancel context.CancelFunc
	done   chan error
}

func New(adapter platform.Adapter, coord *gatewaycoord.Coordinator, routerListen string) (*Facade, error) {
	if adapter == nil {
		adapter = platform.CurrentAdapter
	}
	if coord == nil {
		var err error
		coord, err = gatewaycoord.Default()
		if err != nil {
			return nil, err
		}
	}
	return &Facade{
		adapter:      adapter,
		coord:        coord,
		runtimeDir:   coord.RuntimeDirPath(),
		routerListen: routerListen,
		fatal:        make(chan error, 1),
	}, nil
}

func liveconfigLoadOrBootstrap() (*liveconfig.Source, liveconfig.Config, error) {
	return liveconfig.LoadOrBootstrap("", nil)
}

func (f *Facade) FatalRuntimeErrors() <-chan error {
	return f.fatal
}

func (f *Facade) RuntimeActive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runtime != nil
}

func (f *Facade) SetOwnerCache(cache gatewaycoord.GatewayStateCache) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ownerCache = cache
}

func (f *Facade) PlanStart() (StartPlan, error) {
	f.mu.Lock()
	running := f.runtime != nil
	f.mu.Unlock()
	if running {
		return StartPlan{Kind: StartPlanAlreadyRunning}, nil
	}
	assessment, err := managedpac.Assess(f.adapter)
	if err != nil {
		return StartPlan{}, err
	}
	if assessment.ReplacementRequired {
		return StartPlan{Kind: StartPlanConsentRequired, PACReplacementConsent: f.pacReplacementConsentDetail(assessment)}, nil
	}
	return StartPlan{Kind: StartPlanReady}, nil
}

func (f *Facade) ExecuteStart(ctx context.Context, request StartRequest) (StartResult, error) {
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
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.startMutating = false
		f.mu.Unlock()
	}()

	return gatewayActivation{facade: f}.Start(ctx, request)
}

func (f *Facade) Stop() (StopResult, error) {
	var warnings []CommandWarning
	f.mu.Lock()
	active := f.runtime
	f.runtime = nil
	ownerCache := f.ownerCache
	f.mu.Unlock()
	if active != nil {
		if err := active.engine.CloseTraffic(); err != nil {
			warnings = append(warnings, CommandWarning{Kind: CommandWarningRuntimeCloseFailed, Diagnostic: err.Error()})
		}
		active.cancel()
	}
	var cleanupErr error
	if ownerCache.HTTPRouterListen != "" && ownerCache.Token != "" {
		cleanupErr = cleanup.CleanOwned(f.runtimeDir, f.adapter, ownerCache)
	} else {
		cleanupErr = cleanup.Clean(f.runtimeDir, f.adapter)
	}
	if cleanupErr != nil {
		return StopResult{
			Kind:            StopResultCleanupFailed,
			Warnings:        warnings,
			CleanupFailures: cleanupFailures(cleanupErr),
		}, nil
	}
	return StopResult{Kind: StopResultStopped, Warnings: warnings}, nil
}

func (f *Facade) Status(stale bool) (StatusResult, error) {
	f.mu.Lock()
	active := f.runtime
	ownerCache := f.ownerCache
	f.mu.Unlock()
	result := StatusResult{
		Kind:        GatewayStatusNotRunning,
		Cleanup:     f.cleanupStatus(stale, active != nil, ownerCache),
		InstalledCA: f.installedCAStatus(),
	}
	if active != nil {
		if err := active.pac.RequireLease(); err != nil {
			f.reportFatalRuntimeError(active, err)
			return StatusResult{}, err
		}
		state := active.engine.State()
		result.Kind = GatewayStatusRunning
		result.Owner = &OwnerStatusDetail{RouterListen: f.routerListen}
		result.Runtime = &RuntimeStatusDetail{
			ProxyListen:        state.ProxyListen,
			PACListen:          state.PACListen,
			DomainListPath:     state.DomainList,
			DomainCount:        state.DomainCount,
			CATrusted:          state.CATrusted,
			ManagedPACServices: active.pac.Services(),
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

func (f *Facade) Install() (InstallResult, error) {
	caDir, err := liveconfig.CADir()
	if err != nil {
		return InstallResult{}, err
	}
	_, result, err := userca.Ensure(caDir, f.adapter)
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

func (f *Facade) Uninstall() (UninstallResult, error) {
	if f.RuntimeActive() {
		return UninstallResult{Kind: UninstallResultBlockedRuntimeActive}, nil
	}
	caDir, err := liveconfig.CADir()
	if err != nil {
		return UninstallResult{}, err
	}
	before, _ := userca.Inspect(caDir, f.adapter)
	if err := userca.Uninstall(caDir, f.adapter); err != nil {
		return UninstallResult{}, err
	}
	after, err := userca.Inspect(caDir, f.adapter)
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

func (f *Facade) Check() CheckResult {
	report := f.adapter.Capabilities()
	return CheckResult{
		Platform:          report.Platform,
		Supported:         report.Supported,
		PACManagement:     report.PACManagement,
		CATrustManagement: report.CATrustManagement,
		RuntimeCleanup:    report.RuntimeCleanup,
	}
}

func (f *Facade) watchPACRefreshes(ctx context.Context, active *activeRuntime) {
	for {
		select {
		case <-ctx.Done():
			return
		case nextURL := <-active.engine.PACURLUpdates():
			if err := active.pac.RequireLease(); err != nil {
				f.reportFatalRuntimeError(active, err)
				return
			}
			if err := active.pac.Refresh(nextURL); err != nil {
				f.reportFatalRuntimeError(active, err)
				return
			}
		}
	}
}

func (f *Facade) reportFatalRuntimeError(active *activeRuntime, err error) {
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

func (f *Facade) pacReplacementConsentDetail(assessment managedpac.Assessment) *PACReplacementConsentDetail {
	out := make([]ManagedPACServiceState, 0, len(assessment.States))
	for _, state := range assessment.States {
		out = append(out, ManagedPACServiceState{
			ServiceName: state.ServiceName,
			Enabled:     state.Enabled,
			URL:         state.URL,
			Ownership:   pacOwnership(state.Ownership),
		})
	}
	return &PACReplacementConsentDetail{
		CurrentPACState: out,
		CleanupMode:     CleanupModeNoPACRestoration,
	}
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

func cleanupFailures(err error) []CleanupFailureDetail {
	var cleanupErr cleanup.Error
	if errors.As(err, &cleanupErr) {
		failures := make([]CleanupFailureDetail, 0, len(cleanupErr.Causes))
		for _, cause := range cleanupErr.Causes {
			failures = append(failures, CleanupFailureDetail{
				Subject:    cleanupSubjectForError(cause),
				Diagnostic: cause.Error(),
			})
		}
		return failures
	}
	return []CleanupFailureDetail{{
		Subject:    CleanupSubjectGatewayStateCache,
		Diagnostic: err.Error(),
	}}
}

func cleanupSubjectForError(err error) CleanupSubjectKind {
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "pac") {
		return CleanupSubjectManagedPAC
	}
	return CleanupSubjectGatewayStateCache
}

func (f *Facade) cleanupStatus(stale bool, runtimeActive bool, ownerCache gatewaycoord.GatewayStateCache) CleanupStatusDetail {
	inspection := cleanup.Inspect(f.runtimeDir, f.adapter, stale)
	var subjects []CleanupSubjectKind
	ownerCacheActive := ownerCache.HTTPRouterListen != "" && ownerCache.Token != "" && f.coord.Owns(ownerCache)
	if inspection.StaleGatewayStateCache || (inspection.GatewayStateCache && !ownerCacheActive) {
		subjects = append(subjects, CleanupSubjectGatewayStateCache)
	}
	if inspection.OwnedPAC && !runtimeActive {
		subjects = append(subjects, CleanupSubjectManagedPAC)
	}
	return CleanupStatusDetail{Needed: len(subjects) > 0, Subjects: subjects}
}

func (f *Facade) installedCAStatus() InstalledCAStatusDetail {
	if f.adapter.Capabilities().CATrustManagement != platform.CapabilitySupported {
		return InstalledCAStatusDetail{Health: CAHealthUnsupported}
	}
	caDir, err := liveconfig.CADir()
	if err != nil {
		return InstalledCAStatusDetail{Health: CAHealthUnknown}
	}
	report, err := userca.Inspect(caDir, f.adapter)
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
	case userca.HealthUnsupported:
		return CAHealthUnsupported
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

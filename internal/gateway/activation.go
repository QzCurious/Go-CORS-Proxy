package gateway

import (
	"context"
	"fmt"
	"slices"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

type startSequence struct {
	lifecycle *lifecycle
}

// Execute runs the Start Sequence. UserCA inspection is read-only; trust
// installation remains an explicit lifecycle command.
func (s startSequence) Execute(ctx context.Context, request StartRequest) (result StartResult, resultErr error) {
	var bootstrapErr error
	defer func() {
		if bootstrapErr != nil && result != nil {
			result = withUpstreamListBootstrapWarning(result, bootstrapErr)
		}
	}()

	if !s.lifecycle.takeStartCleanupComplete() {
		if failure := cleanManagedPAC(ctx, s.lifecycle.managedPAC); failure != nil {
			return StartCleanupFailed{Failures: []CleanupFailure{*failure}}, nil
		}
	}

	upstreamListPath, err := defaultUpstreamListPath()
	if err != nil {
		return nil, err
	}
	bootstrap, creationResult, err := assessUpstreamListBootstrap(upstreamListPath, request)
	if err != nil || creationResult != nil {
		return creationResult, err
	}
	if bootstrap {
		bootstrapErr = upstreamlist.Bootstrap(upstreamListPath)
	}
	upstreamListSource := upstreamlist.Open(upstreamListPath)
	closeUpstreamListSource := true
	defer func() {
		if closeUpstreamListSource {
			_ = upstreamListSource.Close()
		}
	}()
	initialUpstreamTransition, ok := <-upstreamListSource.Transitions()
	if !ok {
		return nil, fmt.Errorf("initialize upstream list: source closed before its initial state")
	}

	postStartFailure := func(err error) (StartResult, error) {
		if ctx.Err() != nil {
			return StartStopCancelled{}, nil
		}
		return nil, fmt.Errorf("start runtime: %w", err)
	}

	acceptedServices, assessmentResult, err := s.acceptedManagedPACServices(ctx, request)
	if err != nil || assessmentResult != nil {
		if err != nil {
			return postStartFailure(err)
		}
		return assessmentResult, nil
	}

	engine, err := newRuntime(upstreamListPath, upstreamListSource, initialUpstreamTransition)
	if err != nil {
		return postStartFailure(err)
	}
	closeUpstreamListSource = false
	cleanupEngine := true
	defer func() {
		if cleanupEngine {
			_ = engine.Close()
		}
	}()
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	active := &activeRuntime{
		engine: engine,
		cancel: cancel,
		done:   done,
		phase:  runtimePhaseStarting,
	}

	// UserCA inspection and publication share the CA admission gate so runtime
	// never admits a snapshot from the middle of a UserCA mutation.
	if !s.lifecycle.caAdmissionMu.TryLock() {
		return StartAlreadyMutating{}, nil
	}
	publishRuntime := func() error {
		assessment, readinessErr := s.lifecycle.userCA.Inspect(ctx)
		if err := engine.SetInitialHTTPSReadiness(assessment, readinessErr); err != nil {
			return err
		}
		s.lifecycle.mu.Lock()
		defer s.lifecycle.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.lifecycle.userCASnapshot = assessment.Snapshot()
		s.lifecycle.userCAAssessmentErr = readinessErr
		s.lifecycle.runtime = active
		if _, ok := assessment.Provider(); ok && readinessErr == nil {
			s.lifecycle.scheduleHTTPSDeadlineLocked(active, assessment)
		}
		return nil
	}
	publishErr := publishRuntime()
	s.lifecycle.caAdmissionMu.Unlock()
	if publishErr != nil {
		if ctx.Err() != nil {
			return StartStopCancelled{}, nil
		}
		return postStartFailure(publishErr)
	}
	withdraw := func() {
		s.lifecycle.mu.Lock()
		if s.lifecycle.runtime == active {
			s.lifecycle.runtime = nil
		}
		s.lifecycle.mu.Unlock()
		s.lifecycle.cancelHTTPSDeadline(active)
		cancel()
	}

	// Traffic listeners begin serving before OS PAC state can point at them.
	ready := make(chan struct{})
	go func() {
		err := engine.ServeReady(runCtx, ready)
		done <- err
		if err != nil {
			select {
			case s.lifecycle.fatal <- err:
			default:
			}
		}
	}()
	select {
	case <-ready:
	case err := <-done:
		withdraw()
		return postStartFailure(fmt.Errorf("gateway runtime failed before readiness: %w", err))
	case <-ctx.Done():
		withdraw()
		return StartStopCancelled{}, nil
	}
	select {
	case err := <-done:
		withdraw()
		return postStartFailure(fmt.Errorf("gateway runtime failed before PAC installation: %w", err))
	default:
	}

	// Capture the complete desired PAC input at the installation boundary.
	// Runtime changes may occur while feature-owned installation is in progress;
	// the latest-value desired-state channel retains the newest snapshot for
	// reconciliation once activation has installed its fixed service set.
	pacInstallBaseline := engine.currentDesiredState()
	pacInstall, err := s.lifecycle.managedPAC.InstallDesired(ctx, acceptedServices, pacInstallBaseline)
	if err != nil {
		withdraw()
		warnings := managedPACWarningDetails(pacInstall.Warnings())
		if failure := s.cleanupFailedPACInstall(); failure != nil {
			return StartCleanupFailed{
				Warnings: warnings,
				Failures: []CleanupFailure{*failure},
			}, nil
		}
		if ctx.Err() != nil {
			return StartStopCancelled{}, nil
		}
		return StartManagedPACInstallationFailed{Warnings: warnings, Diagnostic: err.Error()}, nil
	}

	s.lifecycle.mu.Lock()
	if s.lifecycle.runtime != active || ctx.Err() != nil {
		s.lifecycle.mu.Unlock()
		withdraw()
		_ = s.lifecycle.managedPAC.Uninstall(context.Background())
		return StartStopCancelled{}, nil
	}
	active.managedPAC = &managedPACRuntime{
		state:    pacInstall.State(),
		warnings: managedPACWarningDetails(pacInstall.Warnings()),
	}
	active.phase = runtimePhaseRunning
	s.lifecycle.mu.Unlock()
	cleanupEngine = false

	go s.lifecycle.watchRuntimeChanges(runCtx, active, engine.snapshot())

	state := engine.snapshot()
	return Started{Guidance: StartGuidance{
		UpstreamListPath:        upstreamListPath,
		ManagedPACActive:        true,
		ManagedPACServices:      pacInstall.State().ServiceNames(),
		ManagedPACWarnings:      managedPACWarningDetails(pacInstall.Warnings()),
		HTTPSReadiness:          state.HTTPSReadiness,
		HTTPSInterception:       state.HTTPSInterception,
		HTTPSIntent:             state.HTTPSIntent,
		HTTPSWarnings:           state.HTTPSWarnings,
		UpstreamListWarnings:    state.UpstreamListWarnings,
		UpstreamListDegradation: state.UpstreamListDegradation,
	}}, nil
}

func assessUpstreamListBootstrap(path string, request StartRequest) (bool, StartResult, error) {
	assessment := upstreamlist.AssessBootstrap(path)
	if !assessment.Required {
		return false, nil, nil
	}
	if request.UpstreamListCreationConsent == nil {
		return false, StartUpstreamListCreationConsentRequired{Consent: UpstreamListCreationConsent{
			Path: assessment.Path, DefaultContents: assessment.DefaultContents,
			MissingParentDirectories: assessment.MissingParentDirectories,
			Fingerprint:              UpstreamListCreationFingerprint(assessment.Fingerprint),
		}}, nil
	}
	input := request.UpstreamListCreationConsent
	switch input.Decision {
	case UpstreamListCreationDeclined:
		return false, nil, nil
	case UpstreamListCreationAccepted:
		if input.Fingerprint != UpstreamListCreationFingerprint(assessment.Fingerprint) {
			return false, nil, fmt.Errorf("Upstream List creation consent does not match the current bootstrap assessment")
		}
		return true, nil, nil
	default:
		return false, nil, fmt.Errorf("invalid Upstream List creation decision %q", input.Decision)
	}
}

func withUpstreamListBootstrapWarning(result StartResult, err error) StartResult {
	warning := &UpstreamListBootstrapWarningDetail{Cause: err.Error()}
	switch typed := result.(type) {
	case Started:
		typed.UpstreamListBootstrapWarning = warning
		return typed
	case StartConsentRequired:
		typed.UpstreamListBootstrapWarning = warning
		return typed
	case StartNoManageablePACServices:
		typed.UpstreamListBootstrapWarning = warning
		return typed
	case StartManagedPACInstallationFailed:
		typed.UpstreamListBootstrapWarning = warning
		return typed
	case StartAlreadyMutating:
		typed.UpstreamListBootstrapWarning = warning
		return typed
	case StartStopCancelled:
		typed.UpstreamListBootstrapWarning = warning
		return typed
	case StartCleanupFailed:
		typed.UpstreamListBootstrapWarning = warning
		return typed
	default:
		return result
	}
}

func (s startSequence) acceptedManagedPACServices(ctx context.Context, request StartRequest) ([]string, StartResult, error) {
	if request.ManagedPACConsent != nil {
		services := sortedUniqueServiceNames(request.ManagedPACConsent.ServiceNames)
		if len(services) == 0 || request.ManagedPACConsent.Fingerprint != pacConsentFingerprint(services) {
			return nil, nil, fmt.Errorf("Managed PAC Consent does not match its accepted service names")
		}
		return services, nil, nil
	}

	snapshot, err := s.lifecycle.managedPAC.Inspect(ctx)
	if err != nil {
		return nil, nil, err
	}
	detail := s.lifecycle.managedPACConsentDetail(snapshot)
	if len(detail.ProposedServices) == 0 {
		return nil, StartNoManageablePACServices{Consent: *detail}, nil
	}
	return nil, StartConsentRequired{Consent: *detail}, nil
}

func sortedUniqueServiceNames(values []string) []string {
	result := append([]string(nil), values...)
	slices.Sort(result)
	return slices.Compact(result)
}

func (s startSequence) cleanupFailedPACInstall() *CleanupFailureDetail {
	return cleanManagedPAC(context.Background(), s.lifecycle.managedPAC)
}

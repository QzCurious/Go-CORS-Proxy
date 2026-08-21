package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

var errStartCAMutating = errors.New("UserCA is mutating during HTTPS pipeline admission")

type startSequence struct {
	lifecycle *lifecycle
}

// Execute runs the Start Sequence. UserCA inspection is read-only; trust
// installation remains an explicit lifecycle command.
func (s startSequence) Execute(ctx context.Context, request StartRequest) (result StartResult, resultErr error) {
	var creationErr error
	defer func() {
		if creationErr != nil && result != nil {
			result = withUpstreamListCreationWarning(result, creationErr)
		}
	}()

	if !s.lifecycle.takeStartCleanupComplete() {
		if failure := cleanManagedPACActiveState(ctx, s.lifecycle.managedPAC); failure != nil {
			return StartCleanupFailed{Failures: []CleanupFailure{*failure}}, nil
		}
	}

	upstreamListPath, err := defaultUpstreamListPath()
	if err != nil {
		return nil, err
	}
	create, creationResult, err := authorizeUpstreamListCreation(upstreamListPath, request)
	if err != nil || creationResult != nil {
		return creationResult, err
	}
	if create {
		creationErr = createUpstreamList(upstreamListPath)
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
	upstreamListObservation := fileobservation.Open(upstreamListPath)
	closeUpstreamListObservation := true
	defer func() {
		if closeUpstreamListObservation {
			upstreamListObservation.Close()
		}
	}()
	initialUpstreamOutcome := <-upstreamListObservation.Outcomes()

	engine, err := newRuntime(upstreamListPath, upstreamListObservation, initialUpstreamOutcome)
	if err != nil {
		return postStartFailure(err)
	}
	closeUpstreamListObservation = false
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
		ctx:    runCtx,
		cancel: cancel,
		done:   done,
		phase:  runtimePhaseStarting,
	}

	publishRuntime := func() error {
		state := engine.snapshot()
		var assessment userca.Assessment
		var assessmentErr error
		if state.HTTPSPipeline != nil {
			// Only an intent-admitted pipeline inspects UserCA for runtime use.
			// The CA admission gate prevents observing an in-progress mutation.
			if !s.lifecycle.caAdmissionMu.TryLock() {
				return errStartCAMutating
			}
			defer s.lifecycle.caAdmissionMu.Unlock()
			assessment, assessmentErr = s.lifecycle.userCA.Inspect(ctx)
			engine.SetInitialHTTPSAssessment(assessment, assessmentErr)
		}
		s.lifecycle.mu.Lock()
		defer s.lifecycle.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if state.HTTPSPipeline != nil {
			s.lifecycle.userCASnapshot = assessment.Snapshot()
			s.lifecycle.userCAAssessmentErr = assessmentErr
		}
		s.lifecycle.runtime = active
		if engine.interceptionActive() {
			s.lifecycle.scheduleHTTPSDeadlineLocked(active, assessment)
		}
		return nil
	}
	publishErr := publishRuntime()
	if publishErr != nil {
		if errors.Is(publishErr, errStartCAMutating) {
			return StartAlreadyMutating{}, nil
		}
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
	pacInstallBaseline := engine.currentPACProjection()
	pacInstall, err := s.lifecycle.managedPAC.InstallProjection(ctx, acceptedServices, engine.PACListen(), pacInstallBaseline)
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
	var installedCA *InstalledCAStatusDetail
	if state.HTTPSPipeline != nil {
		s.lifecycle.mu.Lock()
		status := installedCAStatus(
			s.lifecycle.userCASnapshot,
			s.lifecycle.userCAAssessmentErr,
			false,
			s.lifecycle.userCACleanupIssue,
		)
		s.lifecycle.mu.Unlock()
		installedCA = &status
	}
	return Started{Guidance: StartGuidance{
		UpstreamListPath:            upstreamListPath,
		ManagedPACActive:            true,
		ManagedPACServices:          pacInstall.State().ServiceNames(),
		ManagedPACPublicationURL:    pacInstall.State().PACURL(),
		ManagedPACWarnings:          managedPACWarningDetails(pacInstall.Warnings()),
		HTTPSPipeline:               state.HTTPSPipeline,
		InstalledCA:                 installedCA,
		UpstreamListWarnings:        state.UpstreamListWarnings,
		UpstreamListFileSyncIssue:   state.UpstreamListFileSyncIssue,
		UpstreamListProjectionIssue: state.UpstreamListProjectionIssue,
	}}, nil
}

func authorizeUpstreamListCreation(path string, request StartRequest) (bool, StartResult, error) {
	consent := assessUpstreamListCreation(path)
	if consent == nil {
		return false, nil, nil
	}
	if request.UpstreamListCreationConsent == nil {
		return false, StartUpstreamListCreationConsentRequired{Consent: *consent}, nil
	}
	input := request.UpstreamListCreationConsent
	switch input.Decision {
	case UpstreamListCreationDeclined:
		return false, nil, nil
	case UpstreamListCreationAccepted:
		if input.Fingerprint != consent.Fingerprint {
			return false, nil, fmt.Errorf("Upstream List creation consent does not match the current creation assessment")
		}
		return true, nil, nil
	default:
		return false, nil, fmt.Errorf("invalid Upstream List creation decision %q", input.Decision)
	}
}

func withUpstreamListCreationWarning(result StartResult, err error) StartResult {
	warning := &UpstreamListCreationWarningDetail{Cause: err.Error()}
	switch typed := result.(type) {
	case Started:
		typed.UpstreamListCreationWarning = warning
		return typed
	case StartConsentRequired:
		typed.UpstreamListCreationWarning = warning
		return typed
	case StartNoManageablePACServices:
		typed.UpstreamListCreationWarning = warning
		return typed
	case StartManagedPACInstallationFailed:
		typed.UpstreamListCreationWarning = warning
		return typed
	case StartAlreadyMutating:
		typed.UpstreamListCreationWarning = warning
		return typed
	case StartStopCancelled:
		typed.UpstreamListCreationWarning = warning
		return typed
	case StartCleanupFailed:
		typed.UpstreamListCreationWarning = warning
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
	return uninstallManagedPAC(context.Background(), s.lifecycle.managedPAC)
}

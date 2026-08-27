package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
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
		_, failure := cleanManagedPACActiveState(ctx, s.lifecycle.managedPAC)
		if failure != nil {
			return StartCleanupFailed{Failures: []CleanupFailure{*failure}}, nil
		}
	}

	globalUpstreamListPath := s.lifecycle.globalUpstreamListPath
	directoryListPath, err := directoryUpstreamListPath(request.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	create, creationResult, err := authorizeUpstreamListCreation(globalUpstreamListPath, request)
	if err != nil || creationResult != nil {
		return creationResult, err
	}
	if create {
		creationErr = createUpstreamList(globalUpstreamListPath)
	}
	postStartFailure := func(err error) (StartResult, error) {
		if ctx.Err() != nil {
			return StartStopCancelled{}, nil
		}
		return nil, fmt.Errorf("start runtime: %w", err)
	}

	managedPACDetail, assessmentResult, err := s.assessManagedPAC(ctx)
	if err != nil || assessmentResult != nil {
		if err != nil {
			return postStartFailure(err)
		}
		return assessmentResult, nil
	}
	globalObservation := fileobservation.Open(globalUpstreamListPath)
	directoryObservation := fileobservation.Open(directoryListPath)
	closeUpstreamListObservations := true
	defer func() {
		if closeUpstreamListObservations {
			globalObservation.Close()
			directoryObservation.Close()
		}
	}()
	initialGlobalOutcome := <-globalObservation.Outcomes()
	initialDirectoryOutcome := <-directoryObservation.Outcomes()

	engine, err := newRuntimeFromSources([]runtimeUpstreamListInput{
		{
			kind:        UpstreamListSourceGlobal,
			path:        globalUpstreamListPath,
			observation: globalObservation,
			initial:     initialGlobalOutcome,
		},
		{
			kind:        UpstreamListSourceDirectory,
			path:        directoryListPath,
			optional:    true,
			observation: directoryObservation,
			initial:     initialDirectoryOutcome,
		},
	}, defaultProxyTransport())
	if err != nil {
		return postStartFailure(err)
	}
	closeUpstreamListObservations = false
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
		var assessment userCAState
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
			s.lifecycle.userCAState = assessment
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
	pacInstall, err := s.lifecycle.managedPAC.InstallProjection(ctx, managedPACDetail.ServiceSet, engine.PACListen(), pacInstallBaseline)
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
		_, _ = s.lifecycle.managedPAC.Uninstall(context.Background())
		return StartStopCancelled{}, nil
	}
	active.managedPAC = &managedPACRuntime{
		state:             pacInstall.State(),
		warnings:          managedPACWarningDetails(pacInstall.Warnings()),
		observationIssues: managedPACObservationIssueDetails(pacInstall.ObservationIssues()),
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
			s.lifecycle.userCAState,
			s.lifecycle.userCAAssessmentErr,
			false,
			s.lifecycle.userCACleanupIssue,
		)
		s.lifecycle.mu.Unlock()
		installedCA = &status
	}
	managedPACDetail.PublicationURL = pacInstall.State().PACURL()
	managedPACDetail.Warnings = managedPACWarningDetails(pacInstall.Warnings())
	managedPACDetail.ObservationIssues = append(
		managedPACDetail.ObservationIssues,
		managedPACObservationIssueDetails(pacInstall.ObservationIssues())...,
	)
	return Started{Guidance: StartGuidance{
		UpstreamLists: state.UpstreamLists,
		ManagedPAC:    managedPACDetail,
		HTTPSPipeline: state.HTTPSPipeline,
		InstalledCA:   installedCA,
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

func (s startSequence) assessManagedPAC(ctx context.Context) (ManagedPACStartDetail, StartResult, error) {
	snapshot, err := s.lifecycle.managedPAC.Inspect(ctx)
	if err != nil {
		return ManagedPACStartDetail{}, nil, err
	}
	detail := managedPACStartDetail(snapshot)
	if len(detail.ServiceSet) == 0 {
		return ManagedPACStartDetail{}, StartNoManageablePACServices{Detail: detail}, nil
	}
	return detail, nil, nil
}

func (s startSequence) cleanupFailedPACInstall() *CleanupFailureDetail {
	_, failure := uninstallManagedPAC(context.Background(), s.lifecycle.managedPAC)
	return failure
}

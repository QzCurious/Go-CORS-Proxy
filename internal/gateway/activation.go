package gateway

import (
	"context"
	"fmt"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
)

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
	if !s.lifecycle.caAdmissionMu.TryLock() {
		return StartAlreadyMutating{}, nil
	}
	userCA, userCAAssessmentErr := s.lifecycle.userCA.Inspect(ctx)
	s.lifecycle.caAdmissionMu.Unlock()
	if ctx.Err() != nil {
		return StartStopCancelled{}, nil
	}
	s.lifecycle.mu.Lock()
	s.lifecycle.userCAState = userCA
	s.lifecycle.userCAAssessmentErr = userCAAssessmentErr
	s.lifecycle.mu.Unlock()

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
	}, defaultProxyTransport(), userCA, userCAAssessmentErr)
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
		s.lifecycle.mu.Lock()
		defer s.lifecycle.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.lifecycle.runtime = active
		if userCAAssessmentErr == nil && userCA.Usable {
			s.lifecycle.scheduleUserCADeadlineLocked(active, userCA)
		}
		return nil
	}
	publishErr := publishRuntime()
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
		s.lifecycle.cancelUserCADeadline(active)
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
		return postStartFailure(fmt.Errorf("gateway runtime failed before System PAC Delivery: %w", err))
	default:
	}

	pacReport, delivered := s.lifecycle.deliverSystemPAC(runCtx, active)
	if !delivered {
		withdraw()
		return StartStopCancelled{}, nil
	}

	s.lifecycle.mu.Lock()
	if s.lifecycle.runtime != active || ctx.Err() != nil {
		s.lifecycle.mu.Unlock()
		withdraw()
		return StartStopCancelled{}, nil
	}
	active.phase = runtimePhaseRunning
	s.lifecycle.mu.Unlock()
	cleanupEngine = false

	go s.lifecycle.watchRuntimeChanges(runCtx, active)

	state := engine.snapshot()
	s.lifecycle.mu.Lock()
	installedCA := installedCAStatus(
		s.lifecycle.userCAState,
		s.lifecycle.userCAAssessmentErr,
		false,
		s.lifecycle.userCACleanupIssue,
	)
	userCAIssue := userCAAssessmentIssue(s.lifecycle.userCAAssessmentErr)
	s.lifecycle.mu.Unlock()
	return Started{Guidance: StartGuidance{
		UpstreamLists: state.UpstreamLists,
		SystemPAC:     pacReport,
		Traffic:       trafficStatus(state, pacReport.RoutesCurrentEndpoint),
		InstalledCA:   installedCA,
		UserCAIssue:   userCAIssue,
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

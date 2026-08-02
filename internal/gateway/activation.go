package gateway

import (
	"context"
	"fmt"
	"slices"

	"github.com/QzCurious/seamless-cors/internal/liveconfig"
)

type startSequence struct {
	lifecycle *lifecycle
}

// Execute runs the Start Sequence. UserCA inspection is read-only; trust
// installation remains an explicit lifecycle command.
func (s startSequence) Execute(ctx context.Context, request StartRequest) (StartResult, error) {
	if !s.lifecycle.takeStartCleanupComplete() {
		if failure := cleanManagedPAC(ctx, s.lifecycle.managedPAC); failure != nil {
			return StartResult{
				Kind:            StartResultCleanupFailed,
				CleanupFailures: []CleanupFailureDetail{*failure},
			}, nil
		}
	}

	config, err := liveconfig.Create()
	if err != nil {
		return StartResult{}, err
	}
	snapshot, err := config.Snapshot()
	if err != nil {
		return StartResult{}, err
	}

	postStartFailure := func(err error) (StartResult, error) {
		if ctx.Err() != nil {
			return StartResult{Kind: StartResultStopCancelled}, nil
		}
		return StartResult{}, &StartError{Diagnostic: err.Error(), Cause: err}
	}

	acceptedServices, assessmentResult, err := s.acceptedManagedPACServices(ctx, request)
	if err != nil || assessmentResult != nil {
		if err != nil {
			return postStartFailure(err)
		}
		return *assessmentResult, nil
	}

	engine, err := newRuntime(config, snapshot)
	if err != nil {
		return postStartFailure(err)
	}
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
		return StartResult{Kind: StartResultStartAlreadyMutating}, nil
	}
	publishRuntime := func() error {
		userCASnapshot, readinessErr := s.lifecycle.userCA.Inspect(ctx)
		if err := engine.SetInitialHTTPSReadiness(userCASnapshot, readinessErr); err != nil {
			return err
		}
		s.lifecycle.mu.Lock()
		defer s.lifecycle.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.lifecycle.userCASnapshot = userCASnapshot
		s.lifecycle.userCAAssessmentErr = readinessErr
		s.lifecycle.runtime = active
		return nil
	}
	publishErr := publishRuntime()
	s.lifecycle.caAdmissionMu.Unlock()
	if publishErr != nil {
		if ctx.Err() != nil {
			return StartResult{Kind: StartResultStopCancelled}, nil
		}
		return postStartFailure(publishErr)
	}
	withdraw := func() {
		s.lifecycle.mu.Lock()
		if s.lifecycle.runtime == active {
			s.lifecycle.runtime = nil
		}
		s.lifecycle.mu.Unlock()
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
		return StartResult{Kind: StartResultStopCancelled}, nil
	}
	select {
	case err := <-done:
		withdraw()
		return postStartFailure(fmt.Errorf("gateway runtime failed before PAC installation: %w", err))
	default:
	}

	pacInstall, err := s.lifecycle.managedPAC.Install(ctx, acceptedServices, engine.PACURL())
	if err != nil {
		withdraw()
		warnings := managedPACWarningDetails(pacInstall.warnings)
		if failure := s.cleanupFailedPACInstall(); failure != nil {
			return StartResult{
				Kind:               StartResultCleanupFailed,
				ManagedPACWarnings: warnings,
				CleanupFailures:    []CleanupFailureDetail{*failure},
			}, nil
		}
		if ctx.Err() != nil {
			return StartResult{Kind: StartResultStopCancelled}, nil
		}
		return StartResult{
			Kind:               StartResultManagedPACInstallationFailed,
			ManagedPACWarnings: warnings,
			Diagnostic:         err.Error(),
		}, nil
	}

	s.lifecycle.mu.Lock()
	if s.lifecycle.runtime != active || ctx.Err() != nil {
		s.lifecycle.mu.Unlock()
		withdraw()
		_ = s.lifecycle.managedPAC.Uninstall(context.Background())
		return StartResult{Kind: StartResultStopCancelled}, nil
	}
	active.managedPAC = &managedPACRuntime{
		state:    pacInstall.state,
		warnings: managedPACWarningDetails(pacInstall.warnings),
	}
	active.phase = runtimePhaseRunning
	s.lifecycle.mu.Unlock()
	cleanupEngine = false

	go s.lifecycle.watchPACReconciliationRequests(runCtx, active)
	go s.lifecycle.watchHTTPSWarningUpdates(runCtx, active)

	state := engine.snapshot()
	return StartResult{
		Kind: StartResultStarted,
		Guidance: &StartGuidanceDetail{
			UpstreamListPath:     snapshot.UpstreamListPath(),
			ManagedPACActive:     true,
			ManagedPACServices:   append([]string(nil), pacInstall.state.services...),
			ManagedPACWarnings:   managedPACWarningDetails(pacInstall.warnings),
			HTTPSReadiness:       state.HTTPSReadiness,
			HTTPSInterception:    state.HTTPSInterception,
			HTTPSIntent:          state.HTTPSIntent,
			HTTPSWarnings:        state.HTTPSWarnings,
			UpstreamListWarnings: upstreamListWarningDetails(snapshot.UpstreamList().Warnings),
		},
	}, nil
}

func (s startSequence) acceptedManagedPACServices(ctx context.Context, request StartRequest) ([]string, *StartResult, error) {
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
		return nil, &StartResult{
			Kind:              StartResultNoManageablePACServices,
			ManagedPACConsent: detail,
		}, nil
	}
	return nil, &StartResult{
		Kind:              StartResultConsentRequired,
		ManagedPACConsent: detail,
	}, nil
}

func sortedUniqueServiceNames(values []string) []string {
	result := append([]string(nil), values...)
	slices.Sort(result)
	return slices.Compact(result)
}

func (s startSequence) cleanupFailedPACInstall() *CleanupFailureDetail {
	return cleanManagedPAC(context.Background(), s.lifecycle.managedPAC)
}

package gateway

import (
	"context"
	"fmt"

	"github.com/QzCurious/seamless-cors/internal/liveconfig"
	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

type startSequence struct {
	lifecycle *lifecycle
}

// Execute runs the Start Sequence. UserCA inspection is read-only; trust
// installation remains an explicit lifecycle command.
func (s startSequence) Execute(ctx context.Context, request StartRequest) (StartResult, error) {
	if !s.lifecycle.takeStartCleanupComplete() {
		if failure := cleanManagedPAC(ctx, s.lifecycle.managedPACSettings); failure != nil {
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

	assessment, err := managedpac.Assess(ctx, s.lifecycle.managedPACSettings)
	if err != nil {
		return postStartFailure(err)
	}
	detail := s.lifecycle.pacReplacementConsentDetail(assessment)
	if assessment.ReplacementRequired && !acceptsPACState(request.PACReplacementConsent, detail.Fingerprint) {
		return StartResult{
			Kind:                  StartResultConsentRequired,
			PACReplacementConsent: detail,
		}, nil
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
	// Assess and publish readiness under the CA admission gate so a concurrent
	// install cannot complete between the assessment and runtime publication.
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
	if publishErr != nil {
		s.lifecycle.caAdmissionMu.Unlock()
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

	s.lifecycle.caAdmissionMu.Unlock()

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

	session, err := managedpac.Prepare(s.lifecycle.managedPACSettings, assessment.ServiceSet, engine.PACURL())
	if err != nil {
		withdraw()
		return postStartFailure(err)
	}
	s.lifecycle.mu.Lock()
	if s.lifecycle.runtime != active || ctx.Err() != nil {
		s.lifecycle.mu.Unlock()
		session.Close()
		withdraw()
		return StartResult{Kind: StartResultStopCancelled}, nil
	}
	active.pac = session
	s.lifecycle.mu.Unlock()
	pacStart, err := session.Install(ctx)
	if err != nil {
		withdraw()
		if failure := s.cleanupFailedPACInstall(); failure != nil {
			return StartResult{
				Kind:            StartResultCleanupFailed,
				CleanupFailures: []CleanupFailureDetail{*failure},
			}, nil
		}
		return postStartFailure(err)
	}

	s.lifecycle.mu.Lock()
	if s.lifecycle.runtime != active || ctx.Err() != nil {
		s.lifecycle.mu.Unlock()
		session.Close()
		withdraw()
		_ = s.cleanupFailedPACInstall()
		return StartResult{Kind: StartResultStopCancelled}, nil
	}
	active.phase = runtimePhaseRunning
	s.lifecycle.mu.Unlock()
	cleanupEngine = false

	go s.lifecycle.watchPACRefreshes(runCtx, active)
	go s.lifecycle.watchHTTPSWarningUpdates(runCtx, active)

	return StartResult{
		Kind: StartResultStarted,
		Guidance: &StartGuidanceDetail{
			UpstreamListPath:     snapshot.UpstreamListPath(),
			ManagedPACActive:     true,
			ManagedPACServices:   pacStart.InstalledServices,
			HTTPSReadiness:       engine.snapshot().HTTPSReadiness,
			HTTPSInterception:    engine.snapshot().HTTPSInterception,
			HTTPSIntent:          engine.snapshot().HTTPSIntent,
			HTTPSWarnings:        engine.snapshot().HTTPSWarnings,
			UpstreamListWarnings: upstreamListWarningDetails(snapshot.UpstreamList().Warnings),
		},
	}, nil
}

func acceptsPACState(input *PACReplacementConsentInput, fingerprint PACConsentFingerprint) bool {
	return input != nil && input.Accepted && input.Fingerprint == fingerprint
}

func (s startSequence) cleanupFailedPACInstall() *CleanupFailureDetail {
	return cleanManagedPAC(context.Background(), s.lifecycle.managedPACSettings)
}

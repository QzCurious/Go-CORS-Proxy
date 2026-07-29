package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/QzCurious/seamless-cors/internal/liveconfig"
	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

type startSequence struct {
	lifecycle *lifecycle
}

// Execute runs the Start Sequence. CA Ensure deliberately completes before PAC
// Replacement Consent is assessed; the two lifecycle operations remain
// independent even though start composes them.
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

	var authority *userca.Authority
	var caEnsure *CAEnsureResult
	var ensuredFingerprint string
	var caDir string
	if snapshot.CATrusted() {
		caDir, err = userca.DefaultDir()
		if err != nil {
			return StartResult{}, err
		}
		var ensured userca.EnsureResult
		authority, ensured, err = userca.EnsureContext(ctx, caDir, s.lifecycle.userCATrustStore)
		if err != nil {
			if errors.Is(err, userca.ErrApprovalDenied) {
				return StartResult{Kind: StartResultPlatformApprovalDenied}, nil
			}
			if ctx.Err() != nil {
				return StartResult{Kind: StartResultStopCancelled}, nil
			}
			return StartResult{}, err
		}
		caEnsure = caEnsureResult(ensured)
		ensuredFingerprint = ensured.Fingerprint
		if ctx.Err() != nil {
			return StartResult{Kind: StartResultStopCancelled, CAEnsure: caEnsure}, nil
		}
	}

	postCAFailure := func(err error) (StartResult, error) {
		if ctx.Err() != nil {
			return StartResult{Kind: StartResultStopCancelled, CAEnsure: caEnsure}, nil
		}
		return StartResult{CAEnsure: caEnsure}, &StartError{Diagnostic: err.Error(), CAEnsure: caEnsure, Cause: err}
	}

	assessment, err := managedpac.Assess(ctx, s.lifecycle.managedPACSettings)
	if err != nil {
		return postCAFailure(err)
	}
	detail := s.lifecycle.pacReplacementConsentDetail(assessment)
	if assessment.ReplacementRequired && !acceptsPACState(request.PACReplacementConsent, detail.Fingerprint) {
		return StartResult{
			Kind:                  StartResultConsentRequired,
			PACReplacementConsent: detail,
			CAEnsure:              caEnsure,
		}, nil
	}

	loadUsableAuthority := func(ctx context.Context) (*userca.Authority, userca.Report, error) {
		dir, err := userca.DefaultDir()
		if err != nil {
			return nil, userca.Report{Health: userca.HealthUnknown}, err
		}
		return userca.LoadUsableContext(ctx, dir, s.lifecycle.userCATrustStore)
	}
	engine, err := newRuntime(config, snapshot, trustedHTTPSAdmission{
		guard:      &s.lifecycle.caAdmissionMu,
		loadUsable: loadUsableAuthority,
	})
	if err != nil {
		return postCAFailure(err)
	}
	cleanupEngine := true
	defer func() {
		if cleanupEngine {
			_ = engine.Close()
		}
	}()
	if err := engine.SetAuthority(authority); err != nil {
		return postCAFailure(err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	active := &activeRuntime{
		engine:   engine,
		snapshot: snapshot,
		cancel:   cancel,
		done:     done,
		phase:    runtimePhaseStarting,
	}
	// This short admission gate coordinates runtime publication with standalone
	// CA mutation. It is not held across the Start Sequence.
	s.lifecycle.caAdmissionMu.Lock()
	s.lifecycle.mu.Lock()
	if ctx.Err() != nil {
		s.lifecycle.mu.Unlock()
		s.lifecycle.caAdmissionMu.Unlock()
		return StartResult{Kind: StartResultStopCancelled, CAEnsure: caEnsure}, nil
	}
	s.lifecycle.runtime = active
	s.lifecycle.mu.Unlock()
	withdraw := func() {
		s.lifecycle.mu.Lock()
		if s.lifecycle.runtime == active {
			s.lifecycle.runtime = nil
		}
		s.lifecycle.mu.Unlock()
		cancel()
	}

	// Trusted Runtime Admission adopts the currently usable authority. This
	// closes the race with independent CA commands without a start-wide lock.
	if snapshot.CATrusted() {
		admitted, report, admissionErr := userca.LoadUsableContext(ctx, caDir, s.lifecycle.userCATrustStore)
		if admissionErr != nil {
			withdraw()
			s.lifecycle.caAdmissionMu.Unlock()
			return postCAFailure(fmt.Errorf("trusted runtime admission failed: %w", admissionErr))
		}
		if err := engine.SetAuthority(admitted); err != nil {
			withdraw()
			s.lifecycle.caAdmissionMu.Unlock()
			return postCAFailure(err)
		}
		if fingerprint, fingerprintErr := admitted.Fingerprint(); fingerprintErr != nil {
			withdraw()
			s.lifecycle.caAdmissionMu.Unlock()
			return postCAFailure(fmt.Errorf("admitted User CA identity unavailable: %w", fingerprintErr))
		} else if fingerprint != ensuredFingerprint {
			caEnsure.Kind = CAEnsureResultAlreadyUsable
		}
		caEnsure.Expires = report.Expires
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
		return postCAFailure(fmt.Errorf("gateway runtime failed before readiness: %w", err))
	case <-ctx.Done():
		withdraw()
		return StartResult{Kind: StartResultStopCancelled, CAEnsure: caEnsure}, nil
	}
	select {
	case err := <-done:
		withdraw()
		return postCAFailure(fmt.Errorf("gateway runtime failed before PAC installation: %w", err))
	default:
	}

	session, err := managedpac.Prepare(s.lifecycle.managedPACSettings, assessment.ServiceSet, engine.PACURL())
	if err != nil {
		withdraw()
		return postCAFailure(err)
	}
	s.lifecycle.mu.Lock()
	if s.lifecycle.runtime != active || ctx.Err() != nil {
		s.lifecycle.mu.Unlock()
		session.Close()
		withdraw()
		return StartResult{Kind: StartResultStopCancelled, CAEnsure: caEnsure}, nil
	}
	active.pac = session
	s.lifecycle.mu.Unlock()
	pacStart, err := session.Install(ctx)
	if err != nil {
		withdraw()
		if failure := s.cleanupFailedPACInstall(); failure != nil {
			return StartResult{
				Kind:            StartResultCleanupFailed,
				CAEnsure:        caEnsure,
				CleanupFailures: []CleanupFailureDetail{*failure},
			}, nil
		}
		return postCAFailure(err)
	}

	s.lifecycle.mu.Lock()
	if s.lifecycle.runtime != active || ctx.Err() != nil {
		s.lifecycle.mu.Unlock()
		session.Close()
		withdraw()
		_ = s.cleanupFailedPACInstall()
		return StartResult{Kind: StartResultStopCancelled, CAEnsure: caEnsure}, nil
	}
	active.phase = runtimePhaseRunning
	s.lifecycle.mu.Unlock()
	cleanupEngine = false

	go s.lifecycle.watchPACRefreshes(runCtx, active)

	return StartResult{
		Kind:     StartResultStarted,
		CAEnsure: caEnsure,
		Guidance: &StartGuidanceDetail{
			ConfigPath:           snapshot.ConfigPath(),
			UpstreamListPath:     snapshot.UpstreamListPath(),
			ManagedPACActive:     true,
			ManagedPACServices:   pacStart.InstalledServices,
			CATrusted:            snapshot.CATrusted(),
			UpstreamListWarnings: upstreamListWarningDetails(snapshot.UpstreamList().Warnings),
		},
	}, nil
}

func acceptsPACState(input *PACReplacementConsentInput, fingerprint PACConsentFingerprint) bool {
	return input != nil && input.Accepted && input.Fingerprint == fingerprint
}

func caEnsureResult(result userca.EnsureResult) *CAEnsureResult {
	kind := CAEnsureResultAlreadyUsable
	if result.Changed {
		kind = CAEnsureResultInstalled
	}
	return &CAEnsureResult{Kind: kind, Expires: result.Expires}
}

func (s startSequence) cleanupFailedPACInstall() *CleanupFailureDetail {
	return cleanManagedPAC(context.Background(), s.lifecycle.managedPACSettings)
}

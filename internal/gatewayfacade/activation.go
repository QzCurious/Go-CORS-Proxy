package gatewayfacade

import (
	"context"
	"errors"
	"fmt"

	"seamless-cors/internal/gatewayruntime"
	"seamless-cors/internal/liveconfig"
	"seamless-cors/internal/managedpac"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

type gatewayActivation struct {
	facade *Facade
}

// Start runs the Start Sequence. CA Ensure deliberately completes before PAC
// Replacement Consent is assessed; the two lifecycle operations remain
// independent even though start composes them.
func (a gatewayActivation) Start(ctx context.Context, request StartRequest) (StartResult, error) {
	if !a.facade.takeStartCleanupComplete() {
		if err := a.facade.adapter.ClearOwnedPAC(); err != nil {
			return StartResult{}, fmt.Errorf("early managed PAC cleanup failed: %w", err)
		}
	}

	source, live, err := liveconfigLoadOrBootstrap()
	if err != nil {
		return StartResult{}, err
	}

	var authority *userca.Authority
	var caEnsure *CAEnsureResult
	var ensuredFingerprint string
	var caDir string
	if live.CATrusted() {
		caDir, err = liveconfig.CADir()
		if err != nil {
			return StartResult{}, err
		}
		var ensured userca.EnsureResult
		authority, ensured, err = userca.EnsureContext(ctx, caDir, a.facade.adapter)
		if err != nil {
			if errors.Is(err, platform.ErrTrustApprovalDenied) {
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

	assessment, err := managedpac.Assess(a.facade.adapter)
	if err != nil {
		return postCAFailure(err)
	}
	detail := a.facade.pacReplacementConsentDetail(assessment)
	if assessment.ReplacementRequired && !acceptsPACState(request.PACReplacementConsent, detail.Fingerprint) {
		return StartResult{
			Kind:                  StartResultConsentRequired,
			PACReplacementConsent: detail,
			CAEnsure:              caEnsure,
		}, nil
	}

	latest, err := liveconfig.LoadExisting(live.ConfigPath())
	if err != nil {
		return postCAFailure(fmt.Errorf("effective configuration revalidation failed: %w", err))
	}
	if !live.CATrusted() && latest.CATrusted() {
		return postCAFailure(fmt.Errorf("ca-trusted became enabled during start; retry to perform CA Ensure before activation"))
	}
	live = latest

	engine, err := gatewayruntime.New(source, live)
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
		engine: engine,
		live:   live,
		cancel: cancel,
		done:   done,
		phase:  runtimePhaseStarting,
	}
	// This short admission gate coordinates runtime publication with standalone
	// CA mutation. It is not held across the Start Sequence.
	a.facade.caAdmissionMu.Lock()
	a.facade.mu.Lock()
	if ctx.Err() != nil {
		a.facade.mu.Unlock()
		a.facade.caAdmissionMu.Unlock()
		return StartResult{Kind: StartResultStopCancelled, CAEnsure: caEnsure}, nil
	}
	a.facade.runtime = active
	a.facade.mu.Unlock()
	withdraw := func() {
		a.facade.mu.Lock()
		if a.facade.runtime == active {
			a.facade.runtime = nil
		}
		a.facade.mu.Unlock()
		cancel()
	}

	// Trusted Runtime Admission adopts the currently usable authority. This
	// closes the race with independent CA commands without a start-wide lock.
	if live.CATrusted() {
		admitted, report, admissionErr := userca.LoadUsableContext(ctx, caDir, a.facade.adapter)
		if admissionErr != nil {
			withdraw()
			a.facade.caAdmissionMu.Unlock()
			return postCAFailure(fmt.Errorf("trusted runtime admission failed: %w", admissionErr))
		}
		if err := engine.SetAuthority(admitted); err != nil {
			withdraw()
			a.facade.caAdmissionMu.Unlock()
			return postCAFailure(err)
		}
		if fingerprint, fingerprintErr := admitted.Fingerprint(); fingerprintErr != nil {
			withdraw()
			a.facade.caAdmissionMu.Unlock()
			return postCAFailure(fmt.Errorf("admitted User CA identity unavailable: %w", fingerprintErr))
		} else if fingerprint != ensuredFingerprint {
			caEnsure.Kind = CAEnsureResultAlreadyUsable
		}
		caEnsure.Expires = report.Expires
	}
	a.facade.caAdmissionMu.Unlock()

	// Traffic listeners begin serving before OS PAC state can point at them.
	ready := make(chan struct{})
	go func() {
		err := engine.ServeReady(runCtx, ready)
		done <- err
		if err != nil {
			select {
			case a.facade.fatal <- err:
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

	session, err := managedpac.Prepare(a.facade.adapter, assessment.ServiceSet, engine.PACURL())
	if err != nil {
		withdraw()
		return postCAFailure(err)
	}
	a.facade.mu.Lock()
	if a.facade.runtime != active || ctx.Err() != nil {
		a.facade.mu.Unlock()
		session.Close()
		withdraw()
		return StartResult{Kind: StartResultStopCancelled, CAEnsure: caEnsure}, nil
	}
	active.pac = session
	a.facade.mu.Unlock()
	pacStart, err := session.Install()
	if err != nil {
		withdraw()
		return postCAFailure(errors.Join(err, a.cleanupFailedPACInstall()))
	}

	a.facade.mu.Lock()
	if a.facade.runtime != active || ctx.Err() != nil {
		a.facade.mu.Unlock()
		session.Close()
		withdraw()
		_ = a.cleanupFailedPACInstall()
		return StartResult{Kind: StartResultStopCancelled, CAEnsure: caEnsure}, nil
	}
	active.phase = runtimePhaseRunning
	a.facade.mu.Unlock()
	cleanupEngine = false

	go a.facade.watchPACRefreshes(runCtx, active)

	return StartResult{
		Kind:     StartResultStarted,
		CAEnsure: caEnsure,
		Guidance: &StartGuidanceDetail{
			ConfigPath:         live.ConfigPath(),
			DomainListPath:     live.DomainListPath(),
			ManagedPACActive:   true,
			ManagedPACServices: pacStart.InstalledServices,
			CATrusted:          live.CATrusted(),
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

func (a gatewayActivation) cleanupFailedPACInstall() error {
	if err := a.facade.adapter.ClearOwnedPAC(); err != nil {
		return fmt.Errorf("cleanup after failed managed PAC install failed: %w", err)
	}
	return nil
}

package gatewayfacade

import (
	"context"
	"errors"
	"fmt"

	"seamless-cors/internal/cleanup"
	"seamless-cors/internal/gatewayruntime"
	"seamless-cors/internal/liveconfig"
	"seamless-cors/internal/managedpac"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

type gatewayActivation struct {
	facade *Facade
}

func (a gatewayActivation) Start(ctx context.Context, request StartRequest) (StartResult, error) {
	assessment, err := managedpac.Assess(a.facade.adapter)
	if err != nil {
		return StartResult{}, err
	}
	if assessment.ReplacementRequired && (request.PACReplacementConsent == nil || !request.PACReplacementConsent.Accepted) {
		return StartResult{
			Kind:                  StartResultConsentRequired,
			PACReplacementConsent: a.facade.pacReplacementConsentDetail(assessment),
		}, nil
	}

	installedCAChanged := false
	var authority *userca.Authority
	source, live, err := liveconfigLoadOrBootstrap()
	if err != nil {
		return StartResult{}, err
	}
	if live.CATrusted() {
		caDir, err := liveconfig.CADir()
		if err != nil {
			return StartResult{}, err
		}
		var ensureResult userca.EnsureResult
		authority, ensureResult, err = userca.Ensure(caDir, a.facade.adapter)
		if err != nil {
			if errors.Is(err, platform.ErrTrustApprovalDenied) {
				return StartResult{Kind: StartResultPlatformApprovalDenied}, nil
			}
			return StartResult{}, err
		}
		installedCAChanged = ensureResult.Changed
	}

	engine, err := gatewayruntime.New(source, live)
	if err != nil {
		return StartResult{}, err
	}
	cleanupEngine := true
	defer func() {
		if cleanupEngine {
			_ = engine.Close()
		}
	}()

	if err := engine.SetAuthority(authority); err != nil {
		return StartResult{}, err
	}

	pacURL := engine.PACURL()
	session, pacStart, err := managedpac.Start(a.facade.adapter, assessment.ServiceSet, pacURL)
	if err != nil {
		return StartResult{}, errors.Join(err, a.cleanupFailedPACInstall())
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	active := &activeRuntime{
		engine: engine,
		live:   live,
		pac:    session,
		cancel: cancel,
		done:   done,
	}
	a.facade.mu.Lock()
	a.facade.runtime = active
	a.facade.mu.Unlock()
	cleanupEngine = false

	go a.facade.watchPACRefreshes(runCtx, active)
	go func() {
		err := engine.Serve(runCtx)
		done <- err
		if err != nil {
			select {
			case a.facade.fatal <- err:
			default:
			}
		}
	}()

	return StartResult{
		Kind: StartResultStarted,
		Guidance: &StartGuidanceDetail{
			ConfigPath:         live.ConfigPath(),
			DomainListPath:     live.DomainListPath(),
			ManagedPACActive:   true,
			ManagedPACServices: pacStart.InstalledServices,
			CATrusted:          live.CATrusted(),
			InstalledCAChanged: installedCAChanged,
		},
	}, nil
}

func (a gatewayActivation) cleanupFailedPACInstall() error {
	if err := cleanup.Clean(a.facade.runtimeDir, a.facade.adapter); err != nil {
		return fmt.Errorf("cleanup after failed managed PAC install failed: %w", err)
	}
	return nil
}

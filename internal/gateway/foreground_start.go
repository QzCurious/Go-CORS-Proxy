package gateway

import (
	"context"
	"fmt"

	"seamless-cors/internal/cleanup"
	"seamless-cors/internal/platform"
)

type StartHooks struct {
	ConfirmPACReplacement func(PACReplacementConsentDetail) (bool, error)
	Started               func(StartResult)
}

func Start(ctx context.Context, adapter platform.Adapter, hooks StartHooks) (StartResult, error) {
	adapter = adapterOrDefault(adapter)
	if err := requireSupported(adapter.Capabilities()); err != nil {
		return StartResult{}, err
	}
	coord, err := defaultCoordinator()
	if err != nil {
		return StartResult{}, err
	}
	if coord.Verify().Status == stateActive {
		return StartResult{Kind: StartResultOwnerAlreadyRunning}, nil
	}
	if err := cleanRuntime(adapter); err != nil {
		return StartResult{}, err
	}
	owner, err := newOwnerWithCoordinator(adapter, coord)
	if err != nil {
		return StartResult{}, err
	}
	owner.lifecycle.MarkStartCleanupComplete()
	var result StartResult
	err = owner.Run(ctx, func(activationCtx context.Context) error {
		start, err := executeAndStart(activationCtx, owner.lifecycle, hooks)
		result = start
		if err != nil {
			return err
		}
		if start.Kind != StartResultStarted {
			return fmt.Errorf("gateway start did not activate runtime: %s", start.Kind)
		}
		return nil
	})
	return result, err
}

func Serve(ctx context.Context, adapter platform.Adapter, ready func()) error {
	adapter = adapterOrDefault(adapter)
	if err := requireSupported(adapter.Capabilities()); err != nil {
		return err
	}
	coord, err := defaultCoordinator()
	if err != nil {
		return err
	}
	if coord.Verify().Status == stateActive {
		return fmt.Errorf("gateway owner already running")
	}
	owner, err := newOwnerWithCoordinator(adapter, coord)
	if err != nil {
		return err
	}
	return owner.Run(ctx, func(context.Context) error {
		if ready != nil {
			ready()
		}
		return nil
	})
}

func executeAndStart(ctx context.Context, lifecycle *lifecycle, hooks StartHooks) (StartResult, error) {
	request := StartRequest{}
	for {
		result, err := lifecycle.ExecuteStart(ctx, request)
		if hooks.Started != nil {
			hooks.Started(result)
		}
		if err != nil {
			return result, err
		}
		switch result.Kind {
		case StartResultStarted, StartResultAlreadyRunning:
			result.Kind = StartResultStarted
			return result, nil
		case StartResultPlatformApprovalDenied:
			return result, platform.ErrTrustApprovalDenied
		case StartResultStopCancelled:
			return result, nil
		case StartResultConsentRequired:
			if result.PACReplacementConsent == nil {
				return result, fmt.Errorf("consent-required start omitted PAC replacement consent detail")
			}
			accepted := false
			if hooks.ConfirmPACReplacement != nil {
				accepted, err = hooks.ConfirmPACReplacement(*result.PACReplacementConsent)
				if err != nil {
					return result, err
				}
			}
			if !accepted {
				result.Kind = StartResultPACReplacementDeclined
				return result, nil
			}
			request.PACReplacementConsent = &PACReplacementConsentInput{
				Accepted:    true,
				Fingerprint: result.PACReplacementConsent.Fingerprint,
			}
		default:
			return result, fmt.Errorf("gateway start did not activate runtime: %s", result.Kind)
		}
	}
}

func cleanRuntime(adapter cleanup.Adapter) error {
	coord, err := defaultCoordinator()
	if err != nil {
		return err
	}
	return cleanGatewayFootprint(adapter, coord, nil)
}

func cleanGatewayFootprint(adapter cleanup.Cleaner, coord *coordinator, ownedCache *stateCache) error {
	var errs []error
	if err := cleanup.Clean(adapter); err != nil {
		errs = append(errs, err)
	}
	if ownedCache != nil && len(errs) > 0 {
		return cleanup.Error{Causes: errs}
	}
	var err error
	if ownedCache == nil {
		err = coord.Remove()
	} else {
		err = coord.RemoveOwned(*ownedCache)
	}
	if err != nil {
		errs = append(errs, fmt.Errorf("gateway state cache cleanup failed: %w", err))
	}
	if len(errs) > 0 {
		return cleanup.Error{Causes: errs}
	}
	return nil
}

func requireSupported(report platform.CapabilityReport) error {
	if report.Supported &&
		report.PACManagement == platform.CapabilitySupported &&
		report.CATrustManagement == platform.CapabilitySupported &&
		report.RuntimeCleanup == platform.CapabilitySupported {
		return nil
	}
	return fmt.Errorf("platform unsupported: run `seamless-cors check` for details")
}

func adapterOrDefault(adapter platform.Adapter) platform.Adapter {
	if adapter == nil {
		return platform.CurrentAdapter
	}
	return adapter
}

package gateway

import (
	"context"
	"errors"
	"fmt"

	"seamless-cors/internal/platform"
)

var errStartNotActivated = errors.New("gateway start did not activate runtime")

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
	failures, err := cleanRuntime(adapter)
	if err != nil {
		return StartResult{}, err
	}
	if len(failures) > 0 {
		return StartResult{Kind: StartResultCleanupFailed, CleanupFailures: failures}, nil
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
			return fmt.Errorf("%w: %s", errStartNotActivated, start.Kind)
		}
		return nil
	})
	if errors.Is(err, errStartNotActivated) {
		err = nil
	}
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
		case StartResultStopCancelled, StartResultCleanupFailed:
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

func cleanRuntime(adapter platform.Adapter) ([]CleanupFailureDetail, error) {
	coord, err := defaultCoordinator()
	if err != nil {
		return nil, err
	}
	return cleanGatewayFootprint(adapter, coord, nil), nil
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

package gateway

import (
	"context"
	"errors"
	"fmt"

	"seamless-cors/internal/managedpac"
	"seamless-cors/internal/userca"
)

var errStartNotActivated = errors.New("gateway start did not activate runtime")

type StartHooks struct {
	ConfirmPACReplacement func(context.Context, PACReplacementConsentDetail) (bool, error)
	Started               func(StartResult)
}

func Start(ctx context.Context, hooks StartHooks) (StartResult, error) {
	return start(ctx, managedpac.NewSystemSettings(), userca.NewTrustStore(), hooks)
}

func start(ctx context.Context, settings managedpac.SystemSettings, trustStore userca.TrustStore, hooks StartHooks) (StartResult, error) {
	coord, err := defaultCoordinator()
	if err != nil {
		return StartResult{}, err
	}
	lease, acquired, err := coord.AcquireOwnershipLease()
	if err != nil {
		return StartResult{}, err
	}
	if !acquired {
		return StartResult{Kind: StartResultOwnerAlreadyRunning}, nil
	}
	releaseLease := true
	defer func() {
		if releaseLease {
			_ = lease.Release()
		}
	}()
	if coord.Verify().Status == stateActive {
		return StartResult{Kind: StartResultOwnerAlreadyRunning}, nil
	}
	failures := cleanGatewayFootprint(ctx, settings, coord, nil)
	if len(failures) > 0 {
		return StartResult{Kind: StartResultCleanupFailed, CleanupFailures: failures}, nil
	}
	owner, err := newOwnerWithCoordinator(settings, trustStore, coord)
	if err != nil {
		return StartResult{}, err
	}
	owner.lease = lease
	releaseLease = false
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

func Serve(ctx context.Context, ready func()) error {
	return serve(ctx, managedpac.NewSystemSettings(), userca.NewTrustStore(), ready)
}

func serve(ctx context.Context, settings managedpac.SystemSettings, trustStore userca.TrustStore, ready func()) error {
	coord, err := defaultCoordinator()
	if err != nil {
		return err
	}
	lease, acquired, err := coord.AcquireOwnershipLease()
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("gateway owner already running")
	}
	releaseLease := true
	defer func() {
		if releaseLease {
			_ = lease.Release()
		}
	}()
	if coord.Verify().Status == stateActive {
		return fmt.Errorf("gateway owner already running")
	}
	owner, err := newOwnerWithCoordinator(settings, trustStore, coord)
	if err != nil {
		return err
	}
	owner.lease = lease
	releaseLease = false
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
			return result, userca.ErrApprovalDenied
		case StartResultStopCancelled, StartResultCleanupFailed:
			return result, nil
		case StartResultConsentRequired:
			if result.PACReplacementConsent == nil {
				return result, fmt.Errorf("consent-required start omitted PAC replacement consent detail")
			}
			accepted := false
			if hooks.ConfirmPACReplacement != nil {
				accepted, err = hooks.ConfirmPACReplacement(ctx, *result.PACReplacementConsent)
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

func cleanRuntime(ctx context.Context, settings managedpac.SystemSettings) ([]CleanupFailureDetail, error) {
	coord, err := defaultCoordinator()
	if err != nil {
		return nil, err
	}
	lease, acquired, err := coord.AcquireOwnershipLease()
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, fmt.Errorf("gateway owner already running; retry after it finishes starting or stopping")
	}
	defer lease.Release()
	return cleanGatewayFootprint(ctx, settings, coord, nil), nil
}

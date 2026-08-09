package gateway

import (
	"context"
	"errors"
	"fmt"
)

var errStartNotActivated = errors.New("gateway start did not activate runtime")

type StartHooks struct {
	ConfirmUpstreamListCreation func(context.Context, UpstreamListCreationConsent) (bool, error)
	ConfirmManagedPAC           func(context.Context, ManagedPACConsentDetail) (bool, error)
	Started                     func(StartResult)
	HTTPSWarningsChanged        func([]HTTPSWarningDetail)
}

func Start(ctx context.Context, hooks StartHooks) (StartResult, error) {
	target, err := discover()
	if err != nil {
		return nil, err
	}
	if target.kind == targetActive {
		return executeAndStartClient(ctx, target.client, hooks)
	}
	ca, err := openSystemUserCA()
	if err != nil {
		return nil, err
	}
	return start(ctx, openSystemManagedPAC(), ca, hooks)
}

func start(ctx context.Context, pac managedPACModule, ca userCAModule, hooks StartHooks) (StartResult, error) {
	coord, err := defaultCoordinator()
	if err != nil {
		return nil, err
	}
	lease, acquired, err := coord.AcquireOwnershipLease()
	if err != nil {
		return nil, err
	}
	if !acquired {
		target, discoverErr := discover()
		if discoverErr != nil {
			return nil, discoverErr
		}
		if target.kind == targetActive {
			return executeAndStartClient(ctx, target.client, hooks)
		}
		return StartOwnerTransition{}, nil
	}
	releaseLease := true
	defer func() {
		if releaseLease {
			_ = lease.Release()
		}
	}()
	if verification := coord.Verify(); verification.Status == stateActive {
		return executeAndStartClient(ctx, newClient(verification.Cache), hooks)
	}
	failures := cleanGatewayFootprint(ctx, pac, coord, nil)
	if len(failures) > 0 {
		return StartCleanupFailed{Failures: failures}, nil
	}
	owner, err := newOwnerWithCoordinator(pac, ca, coord)
	if err != nil {
		return nil, err
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
		if start == nil || start.Kind() != StartResultStarted {
			if start == nil {
				return fmt.Errorf("%w: nil result", errStartNotActivated)
			}
			return fmt.Errorf("%w: %s", errStartNotActivated, start.Kind())
		}
		owner.lifecycle.SetHTTPSWarningsChanged(hooks.HTTPSWarningsChanged)
		return nil
	})
	if errors.Is(err, errStartNotActivated) {
		err = nil
	}
	return result, err
}

func executeAndStartClient(ctx context.Context, client *client, hooks StartHooks) (StartResult, error) {
	return executeStartLoop(ctx, hooks, client.Start)
}

func Serve(ctx context.Context, ready func()) error {
	ca, err := openSystemUserCA()
	if err != nil {
		return err
	}
	return serve(ctx, openSystemManagedPAC(), ca, ready)
}

func serve(ctx context.Context, pac managedPACModule, ca userCAModule, ready func()) error {
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
	owner, err := newOwnerWithCoordinator(pac, ca, coord)
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
	return executeStartLoop(ctx, hooks, lifecycle.ExecuteStart)
}

func executeStartLoop(
	ctx context.Context,
	hooks StartHooks,
	start func(context.Context, StartRequest) (StartResult, error),
) (StartResult, error) {
	request := StartRequest{}
	for {
		result, err := start(ctx, request)
		if hooks.Started != nil {
			hooks.Started(result)
		}
		if err != nil {
			return result, err
		}
		if result == nil {
			return nil, fmt.Errorf("gateway start returned nil result")
		}
		switch typed := result.(type) {
		case StartUpstreamListCreationConsentRequired:
			accepted := false
			if hooks.ConfirmUpstreamListCreation != nil {
				accepted, err = hooks.ConfirmUpstreamListCreation(ctx, typed.Consent)
				if err != nil {
					return result, err
				}
			}
			input := &UpstreamListCreationConsentInput{Decision: UpstreamListCreationDeclined}
			if accepted {
				input.Decision = UpstreamListCreationAccepted
				input.Fingerprint = typed.Consent.Fingerprint
			}
			request.UpstreamListCreationConsent = input
		case Started, AlreadyRunning,
			StartStopCancelled, StartCleanupFailed, StartAlreadyMutating,
			StartNoManageablePACServices, StartManagedPACInstallationFailed,
			StartOwnerTransition, StartConsentDeclined:
			return result, nil
		case StartConsentRequired:
			accepted := false
			if hooks.ConfirmManagedPAC != nil {
				accepted, err = hooks.ConfirmManagedPAC(ctx, typed.Consent)
				if err != nil {
					return result, err
				}
			}
			if !accepted {
				return StartConsentDeclined{}, nil
			}
			request.ManagedPACConsent = &ManagedPACConsentInput{
				ServiceNames: append([]string(nil), typed.Consent.ProposedServices...),
				Fingerprint:  typed.Consent.Fingerprint,
			}
		default:
			return result, fmt.Errorf("gateway start did not activate runtime: %s", result.Kind())
		}
	}
}

func cleanRuntime(ctx context.Context, pac managedPACModule) ([]CleanupFailureDetail, error) {
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
	return cleanGatewayFootprint(ctx, pac, coord, nil), nil
}

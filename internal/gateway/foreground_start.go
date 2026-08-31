package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
)

var errStartNotActivated = errors.New("gateway start did not activate runtime")

type StartHooks struct {
	ConfirmUpstreamListCreation func(context.Context, UpstreamListCreationConsent) (bool, error)
	Started                     func(StartResult)
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
	lock, acquired, err := coord.TryAcquireOwnerLock()
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
	releaseLock := true
	defer func() {
		if releaseLock {
			_ = lock.Release()
		}
	}()
	if verification := coord.Verify(); verification.Status == stateActive {
		return executeAndStartClient(ctx, newClient(verification.Cache), hooks)
	}
	_, failures := cleanGatewayFootprint(ctx, pac, coord, nil)
	if len(failures) > 0 {
		return StartCleanupFailed{Failures: failures}, nil
	}
	owner, err := newOwnerWithCoordinator(pac, ca, coord)
	if err != nil {
		return nil, err
	}
	owner.lock = lock
	releaseLock = false
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
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("gateway owner already running")
	}
	releaseLock := true
	defer func() {
		if releaseLock {
			_ = lock.Release()
		}
	}()
	if coord.Verify().Status == stateActive {
		return fmt.Errorf("gateway owner already running")
	}
	owner, err := newOwnerWithCoordinator(pac, ca, coord)
	if err != nil {
		return err
	}
	owner.lock = lock
	releaseLock = false
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
	workingDirectory, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	request := StartRequest{WorkingDirectory: workingDirectory}
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
			StartOwnerTransition:
			return result, nil
		default:
			return result, fmt.Errorf("gateway start did not activate runtime: %s", result.Kind())
		}
	}
}

func cleanRuntime(ctx context.Context, pac managedPACModule) ([]ManagedPACObservationIssue, []CleanupFailure, error) {
	coord, err := defaultCoordinator()
	if err != nil {
		return nil, nil, err
	}
	lock, acquired, err := coord.TryAcquireOwnerLock()
	if err != nil {
		return nil, nil, err
	}
	if !acquired {
		return nil, nil, fmt.Errorf("gateway owner already running; retry after it finishes starting or stopping")
	}
	defer lock.Release()
	issues, failures := cleanGatewayFootprint(ctx, pac, coord, nil)
	return issues, failures, nil
}

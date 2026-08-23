package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// StartRouterHosted runs the Start Sequence through an existing router-only owner.
func StartRouterHosted(ctx context.Context, request StartRequest) (StartResult, error) {
	target, err := discover()
	if err != nil {
		return nil, err
	}
	if target.kind != targetActive {
		return nil, fmt.Errorf("gateway owner is not running")
	}
	return target.client.Start(ctx, request)
}

// Stop discovers and stops the live owner, or cleans durable gateway state
// locally when no owner can be reached.
func Stop(ctx context.Context) (StopResult, error) {
	return stop(ctx, openSystemManagedPAC())
}

func stop(ctx context.Context, pac managedPACModule) (StopResult, error) {
	target, err := discover()
	if err != nil {
		return StopResult{}, err
	}
	if target.kind != targetActive {
		failures, err := cleanRuntime(ctx, pac)
		if err != nil {
			return StopResult{}, err
		}
		result := StopResult{Kind: StopResultNotRunning}
		if len(failures) > 0 {
			result.Kind = StopResultNotRunningCleanupFailed
			result.CleanupFailures = failures
		}
		return result, nil
	}
	result, err := target.client.Stop(ctx)
	if err != nil {
		return StopResult{}, err
	}
	if result.Kind == StopResultStopped || result.Kind == StopResultCleanupFailed {
		waitForStop(target.cache)
	}
	return result, nil
}

// GatewayStatus returns live owner status when available and otherwise
// inspects local durable state.
func Status(ctx context.Context) (StatusResult, error) {
	return status(ctx, openSystemManagedPAC(), nil)
}

func status(ctx context.Context, pac managedPACModule, ca userCAModule) (StatusResult, error) {
	target, err := discover()
	if err != nil {
		return StatusResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Status(ctx)
	}
	if ca == nil {
		ca = openSystemUserCA()
	}
	coord, err := defaultCoordinator()
	if err != nil {
		return StatusResult{}, err
	}
	lease, acquired, err := coord.AcquireOwnershipLease()
	if err != nil {
		return StatusResult{}, err
	}
	if !acquired {
		retry, rediscoverErr := discover()
		if rediscoverErr != nil {
			return StatusResult{}, rediscoverErr
		}
		if retry.kind == targetActive {
			return retry.client.Status(ctx)
		}
		return StatusResult{Kind: StatusResultOwnerTransition}, nil
	}
	defer lease.Release()
	lifecycle, err := newLifecycle(pac, ca, coord, "")
	if err != nil {
		return StatusResult{}, err
	}
	if lifecycle.userCAAssessmentErr != nil {
		return StatusResult{}, lifecycle.userCAAssessmentErr
	}
	return lifecycle.Status(ctx, target.kind == targetStale)
}

// InstallCA ensures the Installed User CA through the live owner when one is
// available, and locally otherwise.
func InstallCA(ctx context.Context) (InstallResult, error) {
	return installCA(ctx, nil)
}

func installCA(ctx context.Context, ca userCAModule) (InstallResult, error) {
	target, err := discover()
	if err != nil {
		return InstallResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Install(ctx)
	}
	if ca == nil {
		ca = openSystemUserCA()
	}
	result, routed, err := runTransient(ctx, ca, func(lifecycle *lifecycle) (InstallResult, error) {
		return lifecycle.Install(ctx)
	})
	if routed != nil {
		return routed.Install(ctx)
	}
	if errors.Is(err, errOwnerTransition) {
		return InstallResult{Kind: InstallResultOwnerTransition}, nil
	}
	return result, err
}

// UninstallCA removes the Installed User CA through the live owner when one is
// available, and locally otherwise.
func UninstallCA(ctx context.Context, request UninstallRequest) (UninstallResult, error) {
	return uninstallCA(ctx, nil, request)
}

func uninstallCA(ctx context.Context, ca userCAModule, request UninstallRequest) (UninstallResult, error) {
	target, err := discover()
	if err != nil {
		return UninstallResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Uninstall(ctx, request)
	}
	if ca == nil {
		ca = openSystemUserCA()
	}
	result, routed, err := runTransient(ctx, ca, func(lifecycle *lifecycle) (UninstallResult, error) {
		return lifecycle.UninstallWithConsent(ctx, request.ConsentFingerprint)
	})
	if routed != nil {
		return routed.Uninstall(ctx, request)
	}
	if errors.Is(err, errOwnerTransition) {
		return UninstallResult{Kind: UninstallResultOwnerTransition}, nil
	}
	return result, err
}

// runTransient publishes a router-only owner before executing one owner-owned
// CA command. Losing the ownership race causes one rediscovery; callers then
// route to the winner rather than performing local CA work.
func runTransient[T any](
	ctx context.Context,
	ca userCAModule,
	operation func(*lifecycle) (T, error),
) (result T, routed *client, err error) {
	coord, err := defaultCoordinator()
	if err != nil {
		return result, nil, err
	}
	lease, acquired, err := coord.AcquireOwnershipLease()
	if err != nil {
		return result, nil, err
	}
	if !acquired {
		target, rediscoverErr := discover()
		if rediscoverErr != nil {
			return result, nil, rediscoverErr
		}
		if target.kind == targetActive {
			return result, target.client, nil
		}
		return result, nil, fmt.Errorf("%w; retry command", errOwnerTransition)
	}
	releaseLease := true
	defer func() {
		if releaseLease {
			err = errors.Join(err, lease.Release())
		}
	}()
	owner, err := newTransientOwnerWithCoordinator(openSystemManagedPAC(), ca, coord)
	if err != nil {
		return result, nil, err
	}
	owner.lease = lease
	releaseLease = false
	owner.lifecycle.mu.Lock()
	owner.lifecycle.transientOwner = true
	owner.lifecycle.caMutating = true
	owner.lifecycle.mu.Unlock()

	routerErr := make(chan error, 1)
	go func() { routerErr <- owner.router.Serve(owner.listener) }()
	if err := coord.Claim(owner.cache); err != nil {
		_ = owner.router.Close(context.Background())
		_ = lease.Release()
		owner.lease = nil
		return result, nil, err
	}

	result, operationErr := operation(owner.lifecycle)
	owner.lifecycle.mu.Lock()
	owner.lifecycle.ownerEnding = true
	owner.lifecycle.caMutating = false
	owner.lifecycle.mu.Unlock()
	closeErr := owner.router.Close(context.Background())
	removeErr := coord.RemoveOwned(owner.cache)
	leaseErr := lease.Release()
	owner.lease = nil
	serveErr := <-routerErr
	if serveErr != nil && serveErr != http.ErrServerClosed {
		closeErr = errors.Join(closeErr, serveErr)
	}
	return result, nil, errors.Join(operationErr, closeErr, removeErr, leaseErr)
}

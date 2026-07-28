package gateway

import (
	"context"
	"fmt"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

// StartRouterHosted runs the Start Sequence through an existing router-only owner.
func StartRouterHosted(ctx context.Context, request StartRequest) (StartResult, error) {
	target, err := discover()
	if err != nil {
		return StartResult{}, err
	}
	if target.kind != targetActive {
		return StartResult{}, fmt.Errorf("gateway owner is not running")
	}
	return target.client.Start(ctx, request)
}

// Stop discovers and stops the live owner, or cleans durable gateway state
// locally when no owner can be reached.
func Stop(ctx context.Context) (StopResult, error) {
	return stop(ctx, managedpac.NewSystemSettings())
}

func stop(ctx context.Context, settings managedpac.SystemSettings) (StopResult, error) {
	target, err := discover()
	if err != nil {
		return StopResult{}, err
	}
	if target.kind != targetActive {
		failures, err := cleanRuntime(ctx, settings)
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
	if result.Kind == StopResultStopped {
		waitForStop(target.cache)
	}
	return result, nil
}

// GatewayStatus returns live owner status when available and otherwise
// inspects local durable state.
func Status(ctx context.Context) (StatusResult, error) {
	return status(ctx, managedpac.NewSystemSettings(), userca.NewTrustStore())
}

func status(ctx context.Context, settings managedpac.SystemSettings, trustStore userca.TrustStore) (StatusResult, error) {
	target, err := discover()
	if err != nil {
		return StatusResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Status(ctx)
	}
	coord, err := defaultCoordinator()
	if err != nil {
		return StatusResult{}, err
	}
	lifecycle, err := newLifecycle(settings, trustStore, coord, "")
	if err != nil {
		return StatusResult{}, err
	}
	return lifecycle.Status(ctx, target.kind == targetStale)
}

// InstallCA ensures the Installed User CA through the live owner when one is
// available, and locally otherwise.
func InstallCA(ctx context.Context) (InstallResult, error) {
	return installCA(ctx, userca.NewTrustStore())
}

func installCA(ctx context.Context, trustStore userca.TrustStore) (InstallResult, error) {
	target, err := discover()
	if err != nil {
		return InstallResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Install(ctx)
	}
	lifecycle, err := newLocalLifecycle(trustStore)
	if err != nil {
		return InstallResult{}, err
	}
	return lifecycle.Install(ctx)
}

// UninstallCA removes the Installed User CA through the live owner when one is
// available, and locally otherwise.
func UninstallCA(ctx context.Context) (UninstallResult, error) {
	return uninstallCA(ctx, userca.NewTrustStore())
}

func uninstallCA(ctx context.Context, trustStore userca.TrustStore) (UninstallResult, error) {
	target, err := discover()
	if err != nil {
		return UninstallResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Uninstall(ctx)
	}
	lifecycle, err := newLocalLifecycle(trustStore)
	if err != nil {
		return UninstallResult{}, err
	}
	return lifecycle.Uninstall(ctx)
}

func newLocalLifecycle(trustStore userca.TrustStore) (*lifecycle, error) {
	coord, err := defaultCoordinator()
	if err != nil {
		return nil, err
	}
	return newLifecycle(nil, trustStore, coord, "")
}

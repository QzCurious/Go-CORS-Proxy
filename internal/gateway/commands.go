package gateway

import (
	"context"
	"fmt"

	"seamless-cors/internal/platform"
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
	return target.client.Start(request)
}

// Stop discovers and stops the live owner, or cleans durable gateway state
// locally when no owner can be reached.
func Stop(adapter platform.Adapter) (StopResult, error) {
	adapter = adapterOrDefault(adapter)
	target, err := discover()
	if err != nil {
		return StopResult{}, err
	}
	if target.kind != targetActive {
		err := cleanRuntime(adapter)
		result := StopResult{Kind: StopResultNotRunning}
		if err != nil {
			result.CleanupFailures = cleanupFailures(err)
		}
		return result, err
	}
	result, err := target.client.Stop()
	if err != nil {
		return StopResult{}, err
	}
	if result.Kind == StopResultStopped {
		waitForStop(target.cache)
	}
	return result, nil
}

// Check reports managed gateway platform capability without discovering or
// mutating an owner.
func Check(adapter platform.Adapter) CheckResult {
	adapter = adapterOrDefault(adapter)
	report := adapter.Capabilities()
	return CheckResult{
		Platform:          report.Platform,
		Supported:         report.Supported,
		PACManagement:     report.PACManagement,
		CATrustManagement: report.CATrustManagement,
		RuntimeCleanup:    report.RuntimeCleanup,
	}
}

// GatewayStatus returns live owner status when available and otherwise
// inspects local durable state.
func Status(adapter platform.Adapter) (StatusResult, error) {
	adapter = adapterOrDefault(adapter)
	target, err := discover()
	if err != nil {
		return StatusResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Status()
	}
	coord, err := defaultCoordinator()
	if err != nil {
		return StatusResult{}, err
	}
	lifecycle, err := newLifecycle(adapter, coord, "")
	if err != nil {
		return StatusResult{}, err
	}
	return lifecycle.Status(target.kind == targetStale)
}

// InstallCA ensures the Installed User CA through the live owner when one is
// available, and locally otherwise.
func InstallCA(adapter platform.Adapter) (InstallResult, error) {
	adapter = adapterOrDefault(adapter)
	target, err := discover()
	if err != nil {
		return InstallResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Install()
	}
	lifecycle, err := newLocalLifecycle(adapter)
	if err != nil {
		return InstallResult{}, err
	}
	return lifecycle.Install()
}

// UninstallCA removes the Installed User CA through the live owner when one is
// available, and locally otherwise.
func UninstallCA(adapter platform.Adapter) (UninstallResult, error) {
	adapter = adapterOrDefault(adapter)
	target, err := discover()
	if err != nil {
		return UninstallResult{}, err
	}
	if target.kind == targetActive {
		return target.client.Uninstall()
	}
	lifecycle, err := newLocalLifecycle(adapter)
	if err != nil {
		return UninstallResult{}, err
	}
	return lifecycle.Uninstall()
}

func newLocalLifecycle(adapter platform.Adapter) (*lifecycle, error) {
	coord, err := defaultCoordinator()
	if err != nil {
		return nil, err
	}
	return newLifecycle(adapter, coord, "")
}

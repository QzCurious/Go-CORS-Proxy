package gatewayowner

import (
	"context"
	"fmt"

	"seamless-cors/internal/cleanup"
	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayfacade"
	"seamless-cors/internal/liveconfig"
	"seamless-cors/internal/platform"
)

type StartResultKind string

const (
	StartResultStarted                StartResultKind = "started"
	StartResultOwnerAlreadyRunning    StartResultKind = "owner-already-running"
	StartResultPACReplacementDeclined StartResultKind = "pac-replacement-declined"
	StartResultPlatformApprovalDenied StartResultKind = "platform-approval-denied"
)

type StartResult struct {
	Kind   StartResultKind
	Start  *gatewayfacade.StartResult
	Reason error
}

type StartHooks struct {
	ConfirmPACReplacement func(gatewayfacade.PACReplacementConsentDetail) (bool, error)
	Started               func(gatewayfacade.StartResult)
}

func Start(ctx context.Context, adapter platform.Adapter, hooks StartHooks) (StartResult, error) {
	adapter = adapterOrDefault(adapter)
	if err := requireSupported(adapter.Capabilities()); err != nil {
		return StartResult{}, err
	}
	coord, err := gatewaycoord.Default()
	if err != nil {
		return StartResult{}, err
	}
	if coord.Verify().Status == gatewaycoord.Active {
		return StartResult{Kind: StartResultOwnerAlreadyRunning}, nil
	}
	if err := cleanRuntime(adapter); err != nil {
		return StartResult{}, err
	}
	owner, err := NewWithCoord(adapter, coord)
	if err != nil {
		return StartResult{}, err
	}
	var result StartResult
	err = owner.Run(ctx, func() error {
		start, err := planAndStart(ctx, owner.facade, hooks)
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
	coord, err := gatewaycoord.Default()
	if err != nil {
		return err
	}
	if coord.Verify().Status == gatewaycoord.Active {
		return fmt.Errorf("gateway owner already running")
	}
	owner, err := NewWithCoord(adapter, coord)
	if err != nil {
		return err
	}
	return owner.Run(ctx, func() error {
		if ready != nil {
			ready()
		}
		return nil
	})
}

func planAndStart(ctx context.Context, facade *gatewayfacade.Facade, hooks StartHooks) (StartResult, error) {
	plan, err := facade.PlanStart()
	if err != nil {
		return StartResult{}, err
	}
	input := gatewayfacade.StartRequest{}
	if plan.Kind == gatewayfacade.StartPlanConsentRequired && plan.PACReplacementConsent != nil {
		accepted := false
		if hooks.ConfirmPACReplacement != nil {
			accepted, err = hooks.ConfirmPACReplacement(*plan.PACReplacementConsent)
			if err != nil {
				return StartResult{}, err
			}
		}
		if !accepted {
			return StartResult{Kind: StartResultPACReplacementDeclined}, nil
		}
		input.PACReplacementConsent = &gatewayfacade.PACReplacementConsentInput{Accepted: true}
	}
	result, err := facade.ExecuteStart(ctx, input)
	if err != nil {
		return StartResult{}, err
	}
	if hooks.Started != nil {
		hooks.Started(result)
	}
	switch result.Kind {
	case gatewayfacade.StartResultStarted, gatewayfacade.StartResultAlreadyRunning:
		return StartResult{Kind: StartResultStarted, Start: &result}, nil
	case gatewayfacade.StartResultPlatformApprovalDenied:
		return StartResult{Kind: StartResultPlatformApprovalDenied, Start: &result, Reason: platform.ErrTrustApprovalDenied}, platform.ErrTrustApprovalDenied
	case gatewayfacade.StartResultConsentRequired:
		return StartResult{Kind: StartResultPACReplacementDeclined, Start: &result}, nil
	default:
		return StartResult{Start: &result}, fmt.Errorf("gateway start did not activate runtime: %s", result.Kind)
	}
}

func cleanRuntime(adapter cleanup.Adapter) error {
	runtimeDir, err := liveconfig.RuntimeDir()
	if err != nil {
		return err
	}
	return cleanup.Clean(runtimeDir, adapter)
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

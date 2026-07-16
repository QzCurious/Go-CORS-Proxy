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
	StartResultStopCancelled          StartResultKind = "stop-cancelled"
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
	owner.facade.MarkStartCleanupComplete()
	var result StartResult
	err = owner.Run(ctx, func(activationCtx context.Context) error {
		start, err := executeAndStart(activationCtx, owner.facade, hooks)
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
	return owner.Run(ctx, func(context.Context) error {
		if ready != nil {
			ready()
		}
		return nil
	})
}

func executeAndStart(ctx context.Context, facade *gatewayfacade.Facade, hooks StartHooks) (StartResult, error) {
	request := gatewayfacade.StartRequest{}
	for {
		result, err := facade.ExecuteStart(ctx, request)
		if hooks.Started != nil {
			hooks.Started(result)
		}
		if err != nil {
			return StartResult{Start: &result}, err
		}
		switch result.Kind {
		case gatewayfacade.StartResultStarted, gatewayfacade.StartResultAlreadyRunning:
			return StartResult{Kind: StartResultStarted, Start: &result}, nil
		case gatewayfacade.StartResultPlatformApprovalDenied:
			return StartResult{Kind: StartResultPlatformApprovalDenied, Start: &result, Reason: platform.ErrTrustApprovalDenied}, platform.ErrTrustApprovalDenied
		case gatewayfacade.StartResultStopCancelled:
			return StartResult{Kind: StartResultStopCancelled, Start: &result}, nil
		case gatewayfacade.StartResultConsentRequired:
			if result.PACReplacementConsent == nil {
				return StartResult{Start: &result}, fmt.Errorf("consent-required start omitted PAC replacement consent detail")
			}
			accepted := false
			if hooks.ConfirmPACReplacement != nil {
				accepted, err = hooks.ConfirmPACReplacement(*result.PACReplacementConsent)
				if err != nil {
					return StartResult{Start: &result}, err
				}
			}
			if !accepted {
				return StartResult{Kind: StartResultPACReplacementDeclined, Start: &result}, nil
			}
			request.PACReplacementConsent = &gatewayfacade.PACReplacementConsentInput{
				Accepted:    true,
				Fingerprint: result.PACReplacementConsent.Fingerprint,
			}
		default:
			return StartResult{Start: &result}, fmt.Errorf("gateway start did not activate runtime: %s", result.Kind)
		}
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

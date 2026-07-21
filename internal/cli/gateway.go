package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"seamless-cors/internal/gateway"
	"seamless-cors/internal/platform"
)

var ErrPACReplacementConsentDeclined = errors.New("PAC replacement consent declined")

func Start(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return StartWithContextAndInput(ctx, os.Stdin, stdout, platform.CurrentAdapter)
}

func StartWithContext(ctx context.Context, stdout io.Writer, adapter platform.Adapter) error {
	return StartWithContextAndInput(ctx, nil, stdout, adapter)
}

func StartWithContextAndInput(ctx context.Context, stdin io.Reader, stdout io.Writer, adapter platform.Adapter) error {
	stdout = writerOrDiscard(stdout)
	result, err := gateway.Start(ctx, adapter, gateway.StartHooks{
		ConfirmPACReplacement: func(detail gateway.PACReplacementConsentDetail) (bool, error) {
			return confirmPACReplacementConsent(stdin, stdout, &detail)
		},
		Started: func(result gateway.StartResult) {
			renderStartResult(stdout, result)
		},
	})
	if result.Kind == gateway.StartResultOwnerAlreadyRunning {
		fmt.Fprintln(stdout, "gateway owner already running")
		return fmt.Errorf("gateway owner already running")
	}
	if result.Kind == gateway.StartResultPACReplacementDeclined {
		return ErrPACReplacementConsentDeclined
	}
	return err
}

func Serve(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return ServeWithContext(ctx, stdout, platform.CurrentAdapter)
}

func ServeWithContext(ctx context.Context, stdout io.Writer, adapter platform.Adapter) error {
	stdout = writerOrDiscard(stdout)
	return gateway.Serve(ctx, adapter, func() {
		fmt.Fprintln(stdout, "gateway owner running")
	})
}

func Check(stdout, _ io.Writer) error {
	writeCapabilityReport(stdout, gateway.Check(platform.CurrentAdapter))
	return nil
}

func Install(stdout, _ io.Writer) error {
	adapter := platform.CurrentAdapter
	if err := requireSupported(adapter.Capabilities()); err != nil {
		return err
	}
	return InstallCA(stdout, adapter)
}

func Uninstall(stdout, _ io.Writer) error {
	adapter := platform.CurrentAdapter
	report := adapter.Capabilities()
	if report.CATrustManagement != platform.CapabilitySupported {
		return fmt.Errorf("CA trust management is unsupported on this platform")
	}
	return UninstallCA(stdout, adapter)
}

func Stop(stdout, _ io.Writer) error {
	return stop(stdout, platform.CurrentAdapter)
}

func stop(stdout io.Writer, adapter platform.Adapter) error {
	stdout = writerOrDiscard(stdout)
	adapter = adapterOrDefault(adapter)
	result, err := gateway.Stop(adapter)
	if result.Kind == gateway.StopResultNotRunning {
		fmt.Fprintln(stdout, "seamless-cors stop requested; no running seamless-cors found")
		return err
	}
	if err != nil {
		return err
	}
	renderStopResult(stdout, result)
	if result.Kind == gateway.StopResultStopped {
		return nil
	}
	return fmt.Errorf("gateway stop failed: %s", cleanupFailureText(result.CleanupFailures))
}

func Status(stdout, _ io.Writer) error {
	return status(stdout, platform.CurrentAdapter)
}

func status(stdout io.Writer, adapter platform.Adapter) error {
	stdout = writerOrDiscard(stdout)
	adapter = adapterOrDefault(adapter)
	result, err := gateway.Status(adapter)
	if err != nil {
		return err
	}
	renderStatus(stdout, result)
	if result.Kind == gateway.GatewayStatusStaleCache {
		fmt.Fprintln(stdout, "stale Gateway State Cache detected; run start or stop to clean up")
	}
	return nil
}

func InstallCA(stdout io.Writer, adapter platform.Adapter) error {
	stdout = writerOrDiscard(stdout)
	adapter = adapterOrDefault(adapter)
	result, err := gateway.InstallCA(adapter)
	if err != nil {
		if errors.Is(err, platform.ErrTrustApprovalDenied) {
			fmt.Fprintln(stdout, "Certificate trust was not approved.")
			fmt.Fprintln(stdout, "Run the command again and approve the system prompt.")
		}
		return err
	}
	renderInstallResult(stdout, result)
	if result.Kind == gateway.InstallResultBlockedRuntimeActive {
		return fmt.Errorf("Installed User CA replacement blocked while trusted gateway runtime is active")
	}
	return nil
}

func UninstallCA(stdout io.Writer, adapter platform.Adapter) error {
	stdout = writerOrDiscard(stdout)
	adapter = adapterOrDefault(adapter)
	result, err := gateway.UninstallCA(adapter)
	if err != nil {
		return err
	}
	renderUninstallResult(stdout, result)
	if result.Kind == gateway.UninstallResultBlockedRuntimeActive {
		return fmt.Errorf("seamless-cors is running")
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

func writeCapabilityReport(stdout io.Writer, report gateway.CheckResult) {
	fmt.Fprintf(stdout, "platform: %s\n", report.Platform)
	fmt.Fprintf(stdout, "supported: %t\n", report.Supported)
	fmt.Fprintf(stdout, "pac-management: %s\n", report.PACManagement)
	fmt.Fprintf(stdout, "ca-trust-management: %s\n", report.CATrustManagement)
	fmt.Fprintf(stdout, "runtime-cleanup: %s\n", report.RuntimeCleanup)
}

type pacReplacementConsentRequest struct {
	ManagedPAC      bool
	CurrentPACState []platform.PACServiceState
}

func (r pacReplacementConsentRequest) needed() bool {
	return r.ManagedPAC
}

func promptForPACReplacementConsentRequest(stdin io.Reader, stdout io.Writer, req pacReplacementConsentRequest) error {
	if !req.needed() {
		return nil
	}
	detail := &gateway.PACReplacementConsentDetail{
		CurrentPACState: pacStatesForPrompt(req.CurrentPACState),
		CleanupMode:     gateway.CleanupModeNoPACRestoration,
	}
	ok, err := confirmPACReplacementConsent(stdin, stdout, detail)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPACReplacementConsentDeclined
	}
	return nil
}

func confirmPACReplacementConsent(stdin io.Reader, stdout io.Writer, detail *gateway.PACReplacementConsentDetail) (bool, error) {
	if detail == nil {
		return true, nil
	}
	fmt.Fprintln(stdout, "PAC Replacement Consent required before seamless-cors changes current-user OS-managed PAC state.")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "seamless-cors will replace existing managed PAC state for this run.")
	fmt.Fprintln(stdout, "Gateway Footprint Cleanup removes seamless-cors-owned managed PAC settings without restoring previous PAC state.")
	fmt.Fprintln(stdout, "Current managed PAC state:")
	for _, state := range detail.CurrentPACState {
		url := state.URL
		if url == "" {
			url = "(empty)"
		}
		agreement := "included in Managed PAC Service Set"
		if state.ReplacementConsentRequired {
			agreement = "replacement consent required"
		}
		fmt.Fprintf(stdout, "  %s: %s -> seamless-cors owned (%s; enabled=%t url=%s)\n", state.ServiceName, state.Ownership, agreement, state.Enabled, url)
	}
	fmt.Fprintln(stdout)
	fmt.Fprint(stdout, "Proceed? [y/N] ")
	ok, err := readYes(stdin)
	if err != nil {
		return false, err
	}
	if !ok {
		fmt.Fprintln(stdout, "Gateway Activation canceled; any completed UserCA changes are retained.")
		return false, nil
	}
	fmt.Fprintln(stdout)
	return true, nil
}

func readYes(stdin io.Reader) (bool, error) {
	if stdin == nil {
		return true, nil
	}
	answer, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes", nil
}

func pacStatesForPrompt(states []platform.PACServiceState) []gateway.ManagedPACServiceState {
	out := make([]gateway.ManagedPACServiceState, 0, len(states))
	for _, state := range states {
		out = append(out, gateway.ManagedPACServiceState{
			ServiceName:                state.Name,
			Enabled:                    state.Enabled,
			URL:                        state.URL,
			Ownership:                  pacOwnershipForPrompt(state.URL),
			ReplacementConsentRequired: pacOwnershipForPrompt(state.URL) == gateway.PACOwnershipForeign,
		})
	}
	return out
}

func pacOwnershipForPrompt(raw string) gateway.PACOwnership {
	if raw == "" || raw == "(null)" {
		return gateway.PACOwnershipEmpty
	}
	if platform.IsManagedPACFootprint(raw) {
		return gateway.PACOwnershipOwned
	}
	return gateway.PACOwnershipForeign
}

func renderStartResult(stdout io.Writer, result gateway.StartResult) {
	if result.CAEnsure != nil && result.CAEnsure.Kind == gateway.CAEnsureResultInstalled {
		fmt.Fprintln(stdout, "Installed User CA added to the current user's SSL trust settings.")
	}
	switch result.Kind {
	case gateway.StartResultStarted:
		if result.Guidance != nil {
			fmt.Fprintln(stdout, "seamless-cors running")
			fmt.Fprintf(stdout, "config: %s\n", result.Guidance.ConfigPath)
			fmt.Fprintf(stdout, "domain-list: %s\n", result.Guidance.DomainListPath)
			if result.Guidance.ManagedPACActive {
				fmt.Fprintln(stdout, "managed-pac: active")
				if len(result.Guidance.ManagedPACServices) > 0 {
					fmt.Fprintf(stdout, "managed-pac-services: %s\n", strings.Join(result.Guidance.ManagedPACServices, ", "))
				}
			}
		}
	case gateway.StartResultAlreadyRunning:
		fmt.Fprintln(stdout, "seamless-cors already running")
	case gateway.StartResultPlatformApprovalDenied:
		fmt.Fprintln(stdout, "Certificate trust was not approved.")
		fmt.Fprintln(stdout, "Run the command again and approve the system prompt.")
	}
}

func renderStopResult(stdout io.Writer, result gateway.StopResult) {
	switch result.Kind {
	case gateway.StopResultStopped:
		fmt.Fprintln(stdout, "seamless-cors stop requested")
	case gateway.StopResultNotRunning:
		fmt.Fprintln(stdout, "seamless-cors stop requested; no running seamless-cors found")
	case gateway.StopResultCleanupFailed:
		fmt.Fprintln(stdout, "seamless-cors stop cleanup failed")
	}
}

func renderInstallResult(stdout io.Writer, result gateway.InstallResult) {
	switch result.Kind {
	case gateway.InstallResultInstalled:
		fmt.Fprintln(stdout, "Installed User CA installed.")
	case gateway.InstallResultAlreadyUsable:
		fmt.Fprintln(stdout, "Installed User CA is already usable.")
	case gateway.InstallResultBlockedRuntimeActive:
		fmt.Fprintln(stdout, "Installed User CA replacement requires stopping the trusted gateway runtime first.")
	}
	if !result.InstalledCAExpires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.InstalledCAExpires.Format("2006-01-02"))
	}
	for _, advisory := range result.Advisories {
		if advisory.Kind == gateway.InstallAdvisoryConfigCATrustedDisabled && advisory.ConfigCATrustedDisabled != nil {
			fmt.Fprintln(stdout, "HTTPS interception is disabled by config: ca-trusted: false.")
			fmt.Fprintln(stdout, "Set ca-trusted: true to use the Installed User CA.")
		}
	}
}

func renderUninstallResult(stdout io.Writer, result gateway.UninstallResult) {
	switch result.Kind {
	case gateway.UninstallResultUninstalled:
		fmt.Fprintln(stdout, "Installed User CA uninstalled.")
	case gateway.UninstallResultAlreadyAbsent:
		fmt.Fprintln(stdout, "Installed User CA is already absent.")
	case gateway.UninstallResultBlockedRuntimeActive:
		fmt.Fprintln(stdout, "seamless-cors is running; stop it before uninstalling the Installed User CA.")
	}
}

func renderStatus(stdout io.Writer, result gateway.StatusResult) {
	switch result.Kind {
	case gateway.GatewayStatusRunning:
		fmt.Fprintln(stdout, "seamless-cors status: running")
	case gateway.GatewayStatusStarting:
		fmt.Fprintln(stdout, "seamless-cors status: starting")
	case gateway.GatewayStatusRouterOnly:
		fmt.Fprintln(stdout, "seamless-cors status: owner running")
		fmt.Fprintln(stdout, "gateway-runtime: inactive")
	default:
		fmt.Fprintln(stdout, "seamless-cors status: not running")
	}
	if result.Runtime != nil {
		fmt.Fprintf(stdout, "runtime-proxy-endpoint: %s\n", result.Runtime.ProxyListen)
		fmt.Fprintf(stdout, "runtime-pac-endpoint: %s\n", result.Runtime.PACListen)
		if result.Owner != nil {
			fmt.Fprintf(stdout, "gateway-router-endpoint: %s\n", result.Owner.RouterListen)
		}
		fmt.Fprintf(stdout, "domain-list: %s\n", result.Runtime.DomainListPath)
		fmt.Fprintf(stdout, "domains: %d\n", result.Runtime.DomainCount)
		if result.Kind == gateway.GatewayStatusRunning {
			fmt.Fprintln(stdout, "managed-pac: active")
		} else {
			fmt.Fprintln(stdout, "managed-pac: inactive")
		}
		if len(result.Runtime.ManagedPACServices) > 0 {
			fmt.Fprintf(stdout, "managed-pac-services: %s\n", strings.Join(result.Runtime.ManagedPACServices, ", "))
		}
		fmt.Fprintf(stdout, "ca-trusted: %t\n", result.Runtime.CATrusted)
		if len(result.Runtime.PendingLifecycle) > 0 {
			var values []string
			for _, pending := range result.Runtime.PendingLifecycle {
				values = append(values, string(pending))
			}
			fmt.Fprintf(stdout, "pending lifecycle changes: %s\n", strings.Join(values, ", "))
		}
	}
	fmt.Fprintf(stdout, "installed-ca: %s\n", result.InstalledCA.Health)
	if !result.InstalledCA.Expires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.InstalledCA.Expires.Format("2006-01-02"))
	}
	if result.Cleanup.Needed {
		fmt.Fprintln(stdout, "cleanup-needed: run `seamless-cors stop` to clean seamless-cors-owned gateway footprint")
	}
}

func cleanupFailureText(failures []gateway.CleanupFailureDetail) string {
	var parts []string
	for _, failure := range failures {
		if failure.Diagnostic != "" {
			parts = append(parts, failure.Diagnostic)
		} else {
			parts = append(parts, string(failure.Subject))
		}
	}
	return strings.Join(parts, "; ")
}

func writerOrDiscard(stdout io.Writer) io.Writer {
	if stdout == nil {
		return io.Discard
	}
	return stdout
}

func adapterOrDefault(adapter platform.Adapter) platform.Adapter {
	if adapter == nil {
		return platform.CurrentAdapter
	}
	return adapter
}

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/QzCurious/seamless-cors/internal/gateway"
	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

var ErrPACReplacementConsentDeclined = errors.New("PAC replacement consent declined")

type startCommand func(context.Context, gateway.StartHooks) (gateway.StartResult, error)

func Start(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return StartWithContextAndInput(ctx, os.Stdin, stdout)
}

func StartWithContext(ctx context.Context, stdout io.Writer) error {
	return StartWithContextAndInput(ctx, nil, stdout)
}

func StartWithContextAndInput(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	return startWithContextAndInput(ctx, stdin, stdout, gateway.Start)
}

func startWithContextAndInput(ctx context.Context, stdin io.Reader, stdout io.Writer, command startCommand) error {
	stdout = writerOrDiscard(stdout)
	hooks := gateway.StartHooks{
		ConfirmPACReplacement: func(ctx context.Context, detail gateway.PACReplacementConsentDetail) (bool, error) {
			return confirmPACReplacementConsent(ctx, stdin, stdout, &detail)
		},
		Started: func(result gateway.StartResult) {
			renderStartResult(stdout, result)
		},
	}
	result, err := command(ctx, hooks)
	if result.Kind == gateway.StartResultOwnerAlreadyRunning {
		fmt.Fprintln(stdout, "gateway owner already running")
		return fmt.Errorf("gateway owner already running")
	}
	if result.Kind == gateway.StartResultPACReplacementDeclined {
		return ErrPACReplacementConsentDeclined
	}
	if result.Kind == gateway.StartResultCleanupFailed {
		return fmt.Errorf("gateway start cleanup failed: %s", cleanupFailureText(result.CleanupFailures))
	}
	return err
}

type serveCommand func(context.Context, func()) error

func Serve(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return ServeWithContext(ctx, stdout)
}

func ServeWithContext(ctx context.Context, stdout io.Writer) error {
	return serveWithContext(ctx, stdout, gateway.Serve)
}

func serveWithContext(ctx context.Context, stdout io.Writer, command serveCommand) error {
	stdout = writerOrDiscard(stdout)
	ready := func() {
		fmt.Fprintln(stdout, "gateway owner running")
	}
	return command(ctx, ready)
}

type stopCommand func(context.Context) (gateway.StopResult, error)

func Stop(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return stopGateway(ctx, stdout)
}

func stopGateway(ctx context.Context, stdout io.Writer) error {
	return stopGatewayWithCommand(ctx, stdout, gateway.Stop)
}

func stopGatewayWithCommand(ctx context.Context, stdout io.Writer, command stopCommand) error {
	stdout = writerOrDiscard(stdout)
	result, err := command(ctx)
	if err != nil {
		return err
	}
	renderStopResult(stdout, result)
	if result.Kind == gateway.StopResultStopped || result.Kind == gateway.StopResultNotRunning {
		return nil
	}
	return fmt.Errorf("gateway stop failed: %s", cleanupFailureText(result.CleanupFailures))
}

type statusCommand func(context.Context) (gateway.StatusResult, error)

func Status(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return status(ctx, stdout)
}

func status(ctx context.Context, stdout io.Writer) error {
	return statusWithCommand(ctx, stdout, gateway.Status)
}

func statusWithCommand(ctx context.Context, stdout io.Writer, command statusCommand) error {
	stdout = writerOrDiscard(stdout)
	result, err := command(ctx)
	if err != nil {
		return err
	}
	renderStatus(stdout, result)
	if result.Kind == gateway.GatewayStatusStaleCache {
		fmt.Fprintln(stdout, "stale Gateway State Cache detected; run start or stop to clean up")
	}
	return nil
}

type installCACommand func(context.Context) (gateway.InstallResult, error)

func Install(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return InstallCA(ctx, stdout)
}

func InstallCA(ctx context.Context, stdout io.Writer) error {
	return installCAWithCommand(ctx, stdout, gateway.InstallCA)
}

func installCAWithCommand(ctx context.Context, stdout io.Writer, command installCACommand) error {
	stdout = writerOrDiscard(stdout)
	result, err := command(ctx)
	if err != nil {
		if errors.Is(err, userca.ErrApprovalDenied) {
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

type uninstallCACommand func(context.Context) (gateway.UninstallResult, error)

func Uninstall(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return UninstallCA(ctx, stdout)
}

func UninstallCA(ctx context.Context, stdout io.Writer) error {
	return uninstallCAWithCommand(ctx, stdout, gateway.UninstallCA)
}

func uninstallCAWithCommand(ctx context.Context, stdout io.Writer, command uninstallCACommand) error {
	stdout = writerOrDiscard(stdout)
	result, err := command(ctx)
	if err != nil {
		return err
	}
	renderUninstallResult(stdout, result)
	if result.Kind == gateway.UninstallResultBlockedRuntimeActive {
		return fmt.Errorf("seamless-cors is running")
	}
	return nil
}

type pacReplacementConsentRequest struct {
	ManagedPAC      bool
	CurrentPACState []managedpac.ServiceSnapshot
}

func (r pacReplacementConsentRequest) needed() bool {
	return r.ManagedPAC
}

func promptForPACReplacementConsentRequest(ctx context.Context, stdin io.Reader, stdout io.Writer, req pacReplacementConsentRequest) error {
	if !req.needed() {
		return nil
	}
	detail := &gateway.PACReplacementConsentDetail{
		CurrentPACState: pacStatesForPrompt(req.CurrentPACState),
		CleanupMode:     gateway.CleanupModeNoPACRestoration,
	}
	ok, err := confirmPACReplacementConsent(ctx, stdin, stdout, detail)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPACReplacementConsentDeclined
	}
	return nil
}

func confirmPACReplacementConsent(ctx context.Context, stdin io.Reader, stdout io.Writer, detail *gateway.PACReplacementConsentDetail) (bool, error) {
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
	ok, err := readYes(ctx, stdin)
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

func readYes(ctx context.Context, stdin io.Reader) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if stdin == nil {
		return true, nil
	}

	type readResult struct {
		answer string
		err    error
	}
	result := make(chan readResult, 1)
	go func() {
		answer, err := bufio.NewReader(stdin).ReadString('\n')
		result <- readResult{answer: answer, err: err}
	}()

	var answer string
	var err error
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case read := <-result:
		answer, err = read.answer, read.err
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes", nil
}

func pacStatesForPrompt(states []managedpac.ServiceSnapshot) []gateway.ManagedPACServiceState {
	out := make([]gateway.ManagedPACServiceState, 0, len(states))
	for _, state := range states {
		out = append(out, gateway.ManagedPACServiceState{
			ServiceName:                state.ServiceName,
			Enabled:                    state.Enabled,
			URL:                        state.PACURL,
			Ownership:                  pacOwnershipForPrompt(state.PACURL),
			ReplacementConsentRequired: pacOwnershipForPrompt(state.PACURL) == gateway.PACOwnershipForeign,
		})
	}
	return out
}

func pacOwnershipForPrompt(raw string) gateway.PACOwnership {
	switch managedpac.OwnershipForURL(raw) {
	case managedpac.OwnershipEmpty:
		return gateway.PACOwnershipEmpty
	case managedpac.OwnershipOwned:
		return gateway.PACOwnershipOwned
	default:
		return gateway.PACOwnershipForeign
	}
}

func renderStartResult(stdout io.Writer, result gateway.StartResult) {
	if result.CAEnsure != nil && result.CAEnsure.Kind == gateway.CAEnsureResultInstalled {
		fmt.Fprintln(stdout, "Installed User CA added to the current user's SSL trust settings.")
	}
	switch result.Kind {
	case gateway.StartResultStarted:
		if result.Guidance != nil {
			fmt.Fprintln(stdout, "seamless-cors running")
			fmt.Fprintf(stdout, "config: %s\n", homeRelativePath(result.Guidance.ConfigPath))
			fmt.Fprintf(stdout, "upstream-list: %s\n", homeRelativePath(result.Guidance.UpstreamListPath))
			if result.Guidance.ManagedPACActive {
				fmt.Fprintln(stdout, "managed-pac: active")
				if len(result.Guidance.ManagedPACServices) > 0 {
					fmt.Fprintf(stdout, "managed-pac-services: %s\n", strings.Join(result.Guidance.ManagedPACServices, ", "))
				}
			}
			renderUpstreamListWarnings(stdout, result.Guidance.UpstreamListWarnings)
		}
	case gateway.StartResultAlreadyRunning:
		fmt.Fprintln(stdout, "seamless-cors already running")
	case gateway.StartResultPlatformApprovalDenied:
		fmt.Fprintln(stdout, "Certificate trust was not approved.")
		fmt.Fprintln(stdout, "Run the command again and approve the system prompt.")
	case gateway.StartResultCleanupFailed:
		fmt.Fprintln(stdout, "seamless-cors start cleanup failed")
	}
}

func homeRelativePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	relative, err := filepath.Rel(home, path)
	if err != nil {
		return path
	}
	if relative == "." {
		return "~"
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.Join("~", relative)
}

func renderStopResult(stdout io.Writer, result gateway.StopResult) {
	switch result.Kind {
	case gateway.StopResultStopped:
		fmt.Fprintln(stdout, "seamless-cors stop requested")
	case gateway.StopResultNotRunning:
		fmt.Fprintln(stdout, "seamless-cors stop requested; no running seamless-cors found")
	case gateway.StopResultCleanupFailed:
		fmt.Fprintln(stdout, "seamless-cors stop cleanup failed")
	case gateway.StopResultNotRunningCleanupFailed:
		fmt.Fprintln(stdout, "seamless-cors stop cleanup failed; no running seamless-cors found")
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
	case gateway.GatewayStatusEnding:
		fmt.Fprintln(stdout, "seamless-cors status: owner ending")
		fmt.Fprintln(stdout, "gateway-runtime: inactive")
		fmt.Fprintln(stdout, "retry-stop: run `seamless-cors stop` to finish gateway cleanup")
	default:
		fmt.Fprintln(stdout, "seamless-cors status: not running")
	}
	if result.Runtime != nil {
		fmt.Fprintf(stdout, "runtime-proxy-endpoint: %s\n", result.Runtime.ProxyListen)
		fmt.Fprintf(stdout, "runtime-pac-endpoint: %s\n", result.Runtime.PACListen)
		if result.Owner != nil {
			fmt.Fprintf(stdout, "gateway-router-endpoint: %s\n", result.Owner.RouterListen)
		}
		fmt.Fprintf(stdout, "upstream-list: %s\n", result.Runtime.UpstreamListPath)
		fmt.Fprintf(stdout, "upstreams: %d\n", result.Runtime.UpstreamCount)
		renderUpstreamListWarnings(stdout, result.Runtime.UpstreamListWarnings)
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
	if result.Cleanup.State == gateway.CleanupStatusNeeded {
		fmt.Fprintln(stdout, "cleanup-needed: run `seamless-cors stop` to clean seamless-cors-owned gateway footprint")
	} else if result.Cleanup.State == gateway.CleanupStatusUnknown {
		fmt.Fprintln(stdout, "cleanup-status: unknown")
		for _, subject := range result.Cleanup.Subjects {
			if subject.State == gateway.CleanupStatusUnknown && subject.Diagnostic != "" {
				fmt.Fprintf(stdout, "cleanup-%s: unknown: %s\n", subject.Subject, subject.Diagnostic)
			}
		}
	}
}

func renderUpstreamListWarnings(stdout io.Writer, warnings []gateway.UpstreamListWarningDetail) {
	for _, warning := range warnings {
		fmt.Fprintf(
			stdout,
			"warning: upstream-list line %d: %s: %s\n",
			warning.Line,
			warning.Text,
			warning.Diagnostic,
		)
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

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
	"sync"
	"syscall"

	"github.com/QzCurious/seamless-cors/internal/gateway"
)

var ErrManagedPACConsentDeclined = errors.New("Managed PAC consent declined")

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
	liveWarnings := &liveHTTPSWarningRenderer{stdout: stdout}
	hooks := gateway.StartHooks{
		ConfirmManagedPAC: func(ctx context.Context, detail gateway.ManagedPACConsentDetail) (bool, error) {
			return confirmManagedPACConsent(ctx, stdin, stdout, &detail)
		},
		Started: func(result gateway.StartResult) {
			renderStartResultWithoutHTTPSWarnings(stdout, result)
			if result.Guidance != nil {
				liveWarnings.RenderSnapshot(result.Guidance.HTTPSWarnings)
			}
		},
		HTTPSWarningsChanged: liveWarnings.RenderSnapshot,
	}
	result, err := command(ctx, hooks)
	if result.Kind == gateway.StartResultOwnerAlreadyRunning {
		fmt.Fprintln(stdout, "gateway owner already running")
		return fmt.Errorf("gateway owner already running")
	}
	if result.Kind == gateway.StartResultOwnerTransition {
		return fmt.Errorf("Gateway Ownership is transitioning; retry start")
	}
	if result.Kind == gateway.StartResultConsentDeclined {
		return ErrManagedPACConsentDeclined
	}
	if result.Kind == gateway.StartResultCleanupFailed {
		return fmt.Errorf("gateway start cleanup failed: %s", cleanupFailureText(result.CleanupFailures))
	}
	if result.Kind == gateway.StartResultStartAlreadyMutating {
		return fmt.Errorf("CA operation in progress; retry start")
	}
	if result.Kind == gateway.StartResultNoManageablePACServices {
		return fmt.Errorf("gateway start failed: no manageable PAC services")
	}
	if result.Kind == gateway.StartResultManagedPACInstallationFailed {
		return fmt.Errorf("gateway start failed: %s", result.Diagnostic)
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
		if errors.Is(err, gateway.ErrUserCAApprovalDenied) {
			fmt.Fprintln(stdout, "Certificate trust was not approved.")
			fmt.Fprintln(stdout, "Run the command again and approve the system prompt.")
		}
		return err
	}
	renderInstallResult(stdout, result)
	if result.Kind == gateway.InstallResultRuntimeAdoptionFailed {
		return fmt.Errorf("Installed User CA is usable, but Gateway recovery is incomplete")
	}
	return nil
}

type uninstallCACommand func(context.Context, gateway.UninstallRequest) (gateway.UninstallResult, error)

func Uninstall(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return UninstallCAWithInput(ctx, os.Stdin, stdout)
}

func UninstallCA(ctx context.Context, stdout io.Writer) error {
	return UninstallCAWithInput(ctx, os.Stdin, stdout)
}

func UninstallCAWithInput(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	return uninstallCAWithCommand(ctx, stdin, stdout, gateway.UninstallCA)
}

func uninstallCAWithCommand(ctx context.Context, stdin io.Reader, stdout io.Writer, command uninstallCACommand) error {
	stdout = writerOrDiscard(stdout)
	result, err := command(ctx, gateway.UninstallRequest{})
	if err != nil {
		return err
	}
	if result.Kind == gateway.UninstallResultConsentRequired {
		fmt.Fprintln(stdout, "HTTPS interception is active. Uninstalling will disable HTTPS interception and remove every seamless-cors UserCA.")
		fmt.Fprint(stdout, "Proceed? [y/N] ")
		confirmed, err := readYes(ctx, stdin)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(stdout, "Installed User CA uninstall canceled.")
			return nil
		}
		result, err = command(ctx, gateway.UninstallRequest{ConsentFingerprint: result.ConsentFingerprint})
		if err != nil {
			return err
		}
	}
	renderUninstallResult(stdout, result)
	return nil
}

func confirmManagedPACConsent(ctx context.Context, stdin io.Reader, stdout io.Writer, detail *gateway.ManagedPACConsentDetail) (bool, error) {
	if detail == nil {
		return true, nil
	}
	fmt.Fprintln(stdout, "Managed PAC Consent is required before seamless-cors changes current-user OS-managed PAC state.")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "seamless-cors will manage the proposed services for this gateway run.")
	fmt.Fprintln(stdout, "Foreign PAC settings are excluded and will not be changed.")
	fmt.Fprintln(stdout, "Gateway Footprint Cleanup removes seamless-cors-owned managed PAC settings without restoring previous PAC state.")
	fmt.Fprintln(stdout, "Current managed PAC state:")
	for _, state := range detail.CurrentPACState {
		url := state.URL
		if url == "" {
			url = "(empty)"
		}
		agreement := "excluded (foreign PAC state)"
		if state.Manageable {
			agreement = "proposed for Managed PAC Service Set"
		}
		fmt.Fprintf(stdout, "  %s: %s (%s; enabled=%t url=%s)\n", state.ServiceName, state.Ownership, agreement, state.Enabled, url)
	}
	fmt.Fprintln(stdout)
	fmt.Fprint(stdout, "Proceed? [y/N] ")
	ok, err := readYes(ctx, stdin)
	if err != nil {
		return false, err
	}
	if !ok {
		fmt.Fprintln(stdout, "Gateway Activation canceled.")
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

func renderStartResult(stdout io.Writer, result gateway.StartResult) {
	renderStartResultWithHTTPSWarnings(stdout, result, true)
}

func renderStartResultWithoutHTTPSWarnings(stdout io.Writer, result gateway.StartResult) {
	renderStartResultWithHTTPSWarnings(stdout, result, false)
}

func renderStartResultWithHTTPSWarnings(stdout io.Writer, result gateway.StartResult, includeHTTPSWarnings bool) {
	switch result.Kind {
	case gateway.StartResultStarted:
		if result.Guidance != nil {
			fmt.Fprintln(stdout, "seamless-cors running")
			fmt.Fprintf(stdout, "upstream-list: %s\n", homeRelativePath(result.Guidance.UpstreamListPath))
			fmt.Fprintf(stdout, "https: %s\n", humanHTTPSState(result.Guidance.HTTPSInterception))
			if includeHTTPSWarnings {
				renderHTTPSWarnings(stdout, result.Guidance.HTTPSWarnings)
			}
			if result.Guidance.ManagedPACActive {
				fmt.Fprintln(stdout, "managed-pac: active")
				if len(result.Guidance.ManagedPACServices) > 0 {
					fmt.Fprintf(stdout, "managed-pac-services: %s\n", strings.Join(result.Guidance.ManagedPACServices, ", "))
				}
			}
			renderManagedPACWarnings(stdout, result.Guidance.ManagedPACWarnings)
			renderUpstreamListWarnings(stdout, result.Guidance.UpstreamListWarnings)
		}
	case gateway.StartResultAlreadyRunning:
		fmt.Fprintln(stdout, "seamless-cors already running")
	case gateway.StartResultCleanupFailed:
		fmt.Fprintln(stdout, "seamless-cors start cleanup failed")
		renderManagedPACWarnings(stdout, result.ManagedPACWarnings)
	case gateway.StartResultNoManageablePACServices:
		fmt.Fprintln(stdout, "seamless-cors could not start: no manageable PAC services")
		if result.ManagedPACConsent != nil {
			for _, state := range result.ManagedPACConsent.CurrentPACState {
				fmt.Fprintf(stdout, "managed-pac-service: %s (%s)\n", state.ServiceName, state.Ownership)
			}
		}
	case gateway.StartResultManagedPACInstallationFailed:
		fmt.Fprintln(stdout, "seamless-cors could not start: Managed PAC installation failed")
		renderManagedPACWarnings(stdout, result.ManagedPACWarnings)
	}
}

type liveHTTPSWarningRenderer struct {
	mu      sync.Mutex
	stdout  io.Writer
	current map[gateway.HTTPSWarningKind]gateway.HTTPSWarningDetail
}

func (r *liveHTTPSWarningRenderer) RenderSnapshot(warnings []gateway.HTTPSWarningDetail) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := make(map[gateway.HTTPSWarningKind]gateway.HTTPSWarningDetail, len(warnings))
	for _, warning := range warnings {
		next[warning.Kind] = warning
		if previous, ok := r.current[warning.Kind]; !ok || previous != warning {
			renderHTTPSWarnings(r.stdout, []gateway.HTTPSWarningDetail{warning})
		}
	}
	r.current = next
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
		fmt.Fprintln(stdout, "https-readiness: ready")
	case gateway.InstallResultAlreadyUsable:
		fmt.Fprintln(stdout, "Installed User CA is already usable.")
		fmt.Fprintln(stdout, "https-readiness: ready")
	case gateway.InstallResultRuntimeAdoptionFailed:
		fmt.Fprintln(stdout, "Installed User CA installed, but the running Gateway could not adopt it.")
		renderHTTPSWarnings(stdout, result.Warnings)
	}
	if !result.InstalledCAExpires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.InstalledCAExpires.Format("2006-01-02"))
	}
}

func renderUninstallResult(stdout io.Writer, result gateway.UninstallResult) {
	switch result.Kind {
	case gateway.UninstallResultUninstalled:
		fmt.Fprintln(stdout, "Installed User CA uninstalled.")
	case gateway.UninstallResultAlreadyAbsent:
		fmt.Fprintln(stdout, "Installed User CA is already absent.")
	case gateway.UninstallResultConsentRequired:
		fmt.Fprintln(stdout, "Installed User CA uninstall requires confirmation.")
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
		if result.Runtime.ManagedPACActive {
			fmt.Fprintln(stdout, "managed-pac: active")
		} else {
			fmt.Fprintln(stdout, "managed-pac: inactive")
		}
		if len(result.Runtime.ManagedPACServices) > 0 {
			fmt.Fprintf(stdout, "managed-pac-services: %s\n", strings.Join(result.Runtime.ManagedPACServices, ", "))
		}
		renderManagedPACWarnings(stdout, result.Runtime.ManagedPACWarnings)
		fmt.Fprintf(stdout, "https: %s\n", humanHTTPSState(result.Runtime.HTTPSInterception))
		renderHTTPSWarnings(stdout, result.Runtime.HTTPSWarnings)
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

func humanHTTPSState(state gateway.HTTPSInterceptionState) string {
	if state == gateway.HTTPSInterceptionActive {
		return "active"
	}
	return "inactive"
}

func renderHTTPSWarnings(stdout io.Writer, warnings []gateway.HTTPSWarningDetail) {
	for _, warning := range warnings {
		fmt.Fprintf(stdout, "warning: %s\n", warning.Diagnostic)
		if warning.Action != "" {
			fmt.Fprintf(stdout, "action: %s\n", warning.Action)
		}
	}
}

func renderManagedPACWarnings(stdout io.Writer, warnings []gateway.ManagedPACWarningDetail) {
	for _, warning := range warnings {
		if warning.ServiceName == "" {
			fmt.Fprintf(stdout, "managed-pac-warning: %s\n", warning.Diagnostic)
			continue
		}
		fmt.Fprintf(stdout, "managed-pac-warning: %s: %s\n", warning.ServiceName, warning.Diagnostic)
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

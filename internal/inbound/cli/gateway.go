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
)

var forceExit = os.Exit

func foregroundSignalContext() (context.Context, func()) {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go superviseForegroundSignals(signals, done, cancel, forceExit)
	return ctx, func() {
		signal.Stop(signals)
		close(done)
		cancel()
	}
}

func superviseForegroundSignals(signals <-chan os.Signal, done <-chan struct{}, cancel context.CancelFunc, force func(int)) {
	select {
	case <-signals:
		cancel()
	case <-done:
		return
	}
	select {
	case <-signals:
		force(130)
	case <-done:
	}
}

type startCommand func(context.Context, gateway.StartHooks) (gateway.StartResult, error)

func start(stdin io.Reader, stdout, _ io.Writer) error {
	ctx, stop := foregroundSignalContext()
	defer stop()
	return startWithContextAndInput(ctx, stdin, stdout, gateway.Start)
}

func startWithContextAndInput(ctx context.Context, stdin io.Reader, stdout io.Writer, command startCommand) error {
	stdout = writerOrDiscard(stdout)
	hooks := gateway.StartHooks{
		ConfirmUpstreamListCreation: func(ctx context.Context, detail gateway.UpstreamListCreationConsent) (bool, error) {
			return confirmUpstreamListCreation(ctx, stdin, stdout, detail)
		},
		Started: func(result gateway.StartResult) { renderStartResult(stdout, result) },
	}
	result, err := command(ctx, hooks)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("gateway start returned no result")
	}
	if result.Fulfillment() == gateway.CommandFulfilled {
		return nil
	}
	if result.Kind() == gateway.StartResultOwnerTransition {
		return fmt.Errorf("Gateway Ownership is transitioning; retry start")
	}
	if cleanup, ok := result.(gateway.StartCleanupFailed); ok {
		return fmt.Errorf("gateway start cleanup failed: %s", cleanupFailureText(cleanup.Failures))
	}
	if result.Kind() == gateway.StartResultStartAlreadyMutating {
		return fmt.Errorf("CA operation in progress; retry start")
	}
	if result.Kind() == gateway.StartResultNoManageablePACServices {
		return fmt.Errorf("gateway start failed: no manageable PAC services")
	}
	if failed, ok := result.(gateway.StartManagedPACSetFailed); ok {
		return fmt.Errorf("gateway start failed: %s", failed.Diagnostic)
	}
	return fmt.Errorf("gateway start was not fulfilled: %s", result.Kind())
}

func confirmUpstreamListCreation(ctx context.Context, stdin io.Reader, stdout io.Writer, detail gateway.UpstreamListCreationConsent) (bool, error) {
	fmt.Fprintf(stdout, "upstreams.txt is missing: %s\n", detail.Path)
	if len(detail.MissingParentDirectories) > 0 {
		fmt.Fprintf(stdout, "Missing parent directories that will also be created:\n  %s\n", strings.Join(detail.MissingParentDirectories, "\n  "))
	}
	fmt.Fprint(stdout, "Create it? [Y/n] ")
	answer := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdin)
		if scanner.Scan() {
			answer <- scanner.Text()
		} else {
			answer <- ""
		}
	}()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case value := <-answer:
		value = strings.TrimSpace(strings.ToLower(value))
		return value == "" || value == "y" || value == "yes", nil
	}
}

type serveCommand func(context.Context, func()) error

func serve(stdout, _ io.Writer) error {
	ctx, stop := foregroundSignalContext()
	defer stop()
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

func stop(stdout, _ io.Writer) error {
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
	if result.Fulfillment() == gateway.CommandFulfilled {
		return nil
	}
	return fmt.Errorf("gateway stop failed: %s", cleanupFailureText(result.CleanupFailures))
}

type statusCommand func(context.Context) (gateway.StatusResult, error)

func runStatus(stdout, _ io.Writer) error {
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
	if result.Fulfillment() == gateway.CommandUnfulfilled {
		return fmt.Errorf("Gateway Ownership is transitioning; retry status")
	}
	renderStatus(stdout, result)
	if result.State == gateway.GatewayStatusStaleCache {
		fmt.Fprintln(stdout, "stale Gateway State Cache detected; run start or stop to clean up")
	}
	return nil
}

type installCACommand func(context.Context) (gateway.InstallResult, error)

func install(stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return installCAWithCommand(ctx, stdout, gateway.InstallCA)
}

func installCAWithCommand(ctx context.Context, stdout io.Writer, command installCACommand) error {
	stdout = writerOrDiscard(stdout)
	result, err := command(ctx)
	if err != nil {
		return err
	}
	renderInstallResult(stdout, result)
	if result.Fulfillment() == gateway.CommandFulfilled {
		return nil
	}
	switch result.Kind {
	case gateway.InstallResultAlreadyMutating:
		return fmt.Errorf("certificate operation in progress; retry install")
	case gateway.InstallResultOwnerEnding:
		return fmt.Errorf("Gateway owner is ending; retry install")
	case gateway.InstallResultOwnerTransition:
		return fmt.Errorf("Gateway Ownership is transitioning; retry install")
	default:
		return fmt.Errorf("gateway install was not fulfilled: %s", result.Kind)
	}
}

type uninstallCACommand func(context.Context, gateway.UninstallRequest) (gateway.UninstallResult, error)

func uninstall(stdin io.Reader, stdout, _ io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
		confirmed, err := readYes(ctx, stdin, false)
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
	if result.Fulfillment() == gateway.CommandFulfilled {
		return nil
	}
	switch result.Kind {
	case gateway.UninstallResultIncomplete:
		return fmt.Errorf("Installed User CA removal is incomplete")
	case gateway.UninstallResultAlreadyMutating:
		return fmt.Errorf("certificate operation in progress; retry uninstall")
	case gateway.UninstallResultOwnerEnding:
		return fmt.Errorf("Gateway owner is ending; retry uninstall")
	case gateway.UninstallResultOwnerTransition:
		return fmt.Errorf("Gateway Ownership is transitioning; retry uninstall")
	default:
		return fmt.Errorf("gateway uninstall was not fulfilled: %s", result.Kind)
	}
}

func readYes(ctx context.Context, stdin io.Reader, defaultYes bool) (bool, error) {
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
	if answer == "" {
		return defaultYes, nil
	}
	return answer == "y" || answer == "yes", nil
}

func renderStartResult(stdout io.Writer, result gateway.StartResult) {
	if result == nil {
		return
	}
	renderUpstreamListCreationWarning(stdout, result.UpstreamListCreationWarningDetail())
	switch result.Kind() {
	case gateway.StartResultStarted:
		if started, ok := result.(gateway.Started); ok {
			guidance := started.Guidance
			fmt.Fprintln(stdout, "seamless-cors is running.")
			renderStartUpstreamListSources(stdout, guidance.UpstreamLists)
			renderTrafficStatus(stdout, guidance.Traffic)
			renderUserCAAssessmentIssue(stdout, guidance.UserCAIssue)
			renderUserCAInstallGuidance(stdout, guidance.InstalledCA, guidance.UserCAIssue)
			if guidance.InstalledCA.RenewalDue {
				fmt.Fprintln(stdout, "installed-ca-renewal: due")
				fmt.Fprintln(stdout, "action: Run `seamless-cors install` to renew it.")
			}
			if len(guidance.ManagedPAC.ServiceSet) > 0 {
				fmt.Fprintln(stdout, "Services selected for automatic proxy management:")
				for _, service := range guidance.ManagedPAC.ServiceSet {
					fmt.Fprintf(stdout, "  - %s\n", service)
				}
			}
			renderManagedPACExcludedServices(stdout, guidance.ManagedPAC)
			renderStartUpstreamListIssues(stdout, guidance.UpstreamLists)
			renderManagedPACWarnings(stdout, guidance.ManagedPAC.Warnings)
			renderManagedPACObservationIssues(stdout, guidance.ManagedPAC.ObservationIssues)
		}
	case gateway.StartResultAlreadyRunning:
		fmt.Fprintln(stdout, "seamless-cors already running")
	case gateway.StartResultCleanupFailed:
		fmt.Fprintln(stdout, "seamless-cors start cleanup failed")
		if cleanup, ok := result.(gateway.StartCleanupFailed); ok {
			renderManagedPACWarnings(stdout, cleanup.Warnings)
		}
	case gateway.StartResultNoManageablePACServices:
		fmt.Fprintln(stdout, "seamless-cors could not start: no manageable PAC services")
		if noServices, ok := result.(gateway.StartNoManageablePACServices); ok {
			renderManagedPACExcludedServices(stdout, noServices.Detail)
			renderManagedPACObservationIssues(stdout, noServices.Detail.ObservationIssues)
		}
	case gateway.StartResultManagedPACSetFailed:
		fmt.Fprintln(stdout, "seamless-cors could not start: Managed PAC Set failed")
		if failed, ok := result.(gateway.StartManagedPACSetFailed); ok {
			renderManagedPACWarnings(stdout, failed.Warnings)
		}
	}
}

func renderManagedPACExcludedServices(stdout io.Writer, detail gateway.ManagedPACStartDetail) {
	printedHeader := false
	for _, state := range detail.CurrentPACState {
		if state.Manageable {
			continue
		}
		if !printedHeader {
			fmt.Fprintln(stdout, "Network services left unchanged:")
			printedHeader = true
		}
		if state.Ownership == gateway.PACOwnershipUnknown {
			fmt.Fprintf(stdout, "  %s: proxy settings could not be read\n", state.ServiceName)
			continue
		}
		fmt.Fprintf(stdout, "  %s: another PAC configuration is present\n", state.ServiceName)
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
	renderManagedPACObservationIssues(stdout, result.ManagedPACObservationIssues)
}

func renderInstallResult(stdout io.Writer, result gateway.InstallResult) {
	switch result.Kind {
	case gateway.InstallResultInstalled:
		fmt.Fprintln(stdout, "User CA is installed.")
	}
	if !result.InstalledCAExpires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.InstalledCAExpires.Format("2006-01-02"))
	}
}

func renderUninstallResult(stdout io.Writer, result gateway.UninstallResult) {
	switch result.Kind {
	case gateway.UninstallResultUninstalled:
		fmt.Fprintln(stdout, "User CA is uninstalled.")
	case gateway.UninstallResultConsentRequired:
		fmt.Fprintln(stdout, "Installed User CA uninstall requires confirmation.")
	case gateway.UninstallResultIncomplete:
		fmt.Fprintln(stdout, "Installed User CA uninstall is incomplete.")
		if result.CleanupIssue != nil {
			fmt.Fprintf(stdout, "installed-ca-cleanup-issue: %s\n", result.CleanupIssue.Cause)
			if result.CleanupIssue.Action != "" {
				fmt.Fprintf(stdout, "action: %s\n", result.CleanupIssue.Action)
			}
		}
	}
}

func renderStatus(stdout io.Writer, result gateway.StatusResult) {
	switch result.State {
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
		renderUpstreamListSources(stdout, result.Runtime.UpstreamLists)
		fmt.Fprintf(stdout, "upstreams: %d\n", result.Runtime.UpstreamCount)
		if result.Runtime.ManagedPACActive {
			fmt.Fprintln(stdout, "managed-pac: active")
		} else {
			fmt.Fprintln(stdout, "managed-pac: inactive")
		}
		if len(result.Runtime.ManagedPACServices) > 0 {
			fmt.Fprintf(stdout, "managed-pac-services: %s\n", strings.Join(result.Runtime.ManagedPACServices, ", "))
		}
		renderManagedPACWarnings(stdout, result.Runtime.ManagedPACWarnings)
		renderManagedPACObservationIssues(stdout, result.Runtime.ManagedPACObservationIssues)
		renderTrafficStatus(stdout, result.Runtime.Traffic)
	}
	fmt.Fprintf(stdout, "installed-ca: %s\n", result.InstalledCA.Health)
	if !result.InstalledCA.Expires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.InstalledCA.Expires.Format("2006-01-02"))
	}
	if result.InstalledCA.RenewalDue {
		fmt.Fprintln(stdout, "installed-ca-renewal: due")
	}
	if result.InstalledCA.CleanupIssue != nil {
		fmt.Fprintf(stdout, "installed-ca-cleanup-issue: %s\n", result.InstalledCA.CleanupIssue.Cause)
		if result.InstalledCA.CleanupIssue.Action != "" {
			fmt.Fprintf(stdout, "action: %s\n", result.InstalledCA.CleanupIssue.Action)
		}
	}
	renderUserCAAssessmentIssue(stdout, result.UserCAAssessmentIssue)
	renderUserCAInstallGuidance(stdout, result.InstalledCA, result.UserCAAssessmentIssue)
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

func renderTrafficStatus(stdout io.Writer, status gateway.TrafficStatusDetail) {
	fmt.Fprintf(stdout, "traffic-routing-ready: %t\n", status.RoutingReady)
	fmt.Fprintf(stdout, "traffic-projection-current: %t\n", status.ProjectionCurrent)
	fmt.Fprintf(stdout, "http-cors: %s\n", status.HTTPCORS)
	fmt.Fprintf(stdout, "https-cors: %s\n", status.HTTPSCORS)
	fmt.Fprintf(stdout, "https-facade: %s\n", status.HTTPSFacade)
}

func renderUserCAAssessmentIssue(stdout io.Writer, issue *gateway.UserCAAssessmentIssue) {
	if issue != nil {
		fmt.Fprintf(stdout, "userca-assessment-issue: %s\n", issue.Cause)
	}
}

func renderUserCAInstallGuidance(stdout io.Writer, installed gateway.InstalledCAStatusDetail, issue *gateway.UserCAAssessmentIssue) {
	if issue == nil && installed.Health == gateway.CAHealthNotUsable {
		fmt.Fprintln(stdout, "action: Run `seamless-cors install` to install or repair the User CA.")
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

func renderManagedPACObservationIssues(stdout io.Writer, issues []gateway.ManagedPACObservationIssue) {
	for _, issue := range issues {
		if issue.ServiceName == "" {
			fmt.Fprintf(stdout, "managed-pac-observation-issue: %s\n", issue.Diagnostic)
			continue
		}
		fmt.Fprintf(stdout, "managed-pac-observation-issue: %s: %s\n", issue.ServiceName, issue.Diagnostic)
	}
}

func renderUpstreamListWarnings(stdout io.Writer, warnings []gateway.UpstreamListWarningDetail) {
	for _, warning := range warnings {
		fmt.Fprintf(
			stdout,
			"warning: %s %s:%d: %s: %s\n",
			warning.Source,
			warning.Path,
			warning.Line,
			warning.Text,
			warning.Diagnostic,
		)
	}
}

func renderUpstreamListSources(stdout io.Writer, sources []gateway.UpstreamListSourceDetail) {
	for _, source := range sources {
		fmt.Fprintf(stdout, "upstream-list-%s: %s\n", source.Kind, source.Path)
		renderFileSyncIssue(stdout, source.Kind, source.Path, source.FileSyncIssue)
		renderUpstreamListProjectionIssue(stdout, source.Kind, source.Path, source.ProjectionIssue)
		renderUpstreamListWarnings(stdout, source.Warnings)
	}
}

func renderStartUpstreamListSources(stdout io.Writer, sources []gateway.UpstreamListSourceDetail) {
	if len(sources) == 0 {
		return
	}
	fmt.Fprintln(stdout, "Upstream lists:")
	for _, source := range sources {
		label := "Directory"
		if source.Kind == gateway.UpstreamListSourceGlobal {
			label = "Global"
		}
		fmt.Fprintf(stdout, "  %s: %s\n", label, source.Path)
	}
}

func renderStartUpstreamListIssues(stdout io.Writer, sources []gateway.UpstreamListSourceDetail) {
	for _, source := range sources {
		renderFileSyncIssue(stdout, source.Kind, source.Path, source.FileSyncIssue)
		renderUpstreamListProjectionIssue(stdout, source.Kind, source.Path, source.ProjectionIssue)
		renderUpstreamListWarnings(stdout, source.Warnings)
	}
}

func renderUpstreamListCreationWarning(stdout io.Writer, warning *gateway.UpstreamListCreationWarningDetail) {
	if warning == nil {
		return
	}
	fmt.Fprintf(stdout, "warning: upstream-list creation failed: %s\n", warning.Cause)
}

func renderFileSyncIssue(stdout io.Writer, source gateway.UpstreamListSourceKind, path string, issue *gateway.FileSyncIssue) {
	if issue == nil {
		return
	}
	if issue.Kind == gateway.FileSyncIssueObservationStopped {
		fmt.Fprintf(stdout, "warning: %s %s observation stopped: %s\n", source, path, issue.Cause)
		fmt.Fprintln(stdout, "action: repair the cause and restart seamless-cors")
		return
	}
	fmt.Fprintf(stdout, "warning: %s %s unreadable: %s\n", source, path, issue.Cause)
	fmt.Fprintln(stdout, "action: restore the upstream-list file; observation will resume automatically")
}

func renderUpstreamListProjectionIssue(stdout io.Writer, source gateway.UpstreamListSourceKind, path string, issue *gateway.UpstreamListProjectionIssue) {
	if issue == nil {
		return
	}
	fmt.Fprintf(stdout, "warning: %s %s contents rejected: %s\n", source, path, issue.Cause)
}

func cleanupFailureText(failures []gateway.CleanupFailure) string {
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

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/gateway"
	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

func TestPACReplacementConsentPromptReportsManagedPACOnly(t *testing.T) {
	var out bytes.Buffer
	err := promptForPACReplacementConsentRequest(context.Background(), bytes.NewBufferString("yes"), &out, pacReplacementConsentRequest{
		ManagedPAC: true,
		CurrentPACState: []managedpac.ServiceSnapshot{{
			ServiceName: "Wi-Fi",
			PACURL:      "http://corp.example/proxy.pac",
			Enabled:     true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PAC Replacement Consent required", "foreign -> seamless-cors owned", "Proceed? [y/N]"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("consent prompt missing %q:\n%s", want, out.String())
		}
	}
}

func TestPACReplacementConsentPromptDeclineCancels(t *testing.T) {
	var out bytes.Buffer
	err := promptForPACReplacementConsentRequest(context.Background(), bytes.NewBufferString("no"), &out, pacReplacementConsentRequest{ManagedPAC: true})
	if !errors.Is(err, ErrPACReplacementConsentDeclined) {
		t.Fatalf("prompt error = %v", err)
	}
	if !strings.Contains(out.String(), "Gateway Activation canceled") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestPACReplacementConsentPromptReturnsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	input, output := io.Pipe()
	defer input.Close()
	defer output.Close()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := confirmPACReplacementConsent(ctx, input, &out, &gateway.PACReplacementConsentDetail{})
		done <- err
	}()

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("prompt error = %v, want context canceled", err)
	}
	if strings.Contains(out.String(), "Gateway Activation canceled") {
		t.Fatalf("cancelled prompt rendered explicit-decline message: %q", out.String())
	}
}

func TestStartCommandRendersSurfaceNeutralResult(t *testing.T) {
	var out bytes.Buffer
	ctx := context.WithValue(context.Background(), testContextKey{}, "start")
	err := startWithContextAndInput(ctx, bytes.NewBufferString("yes"), &out, func(got context.Context, hooks gateway.StartHooks) (gateway.StartResult, error) {
		if got.Value(testContextKey{}) != "start" {
			t.Fatal("start context was not forwarded")
		}
		result := gateway.StartResult{
			Kind: gateway.StartResultStarted,
			Guidance: &gateway.StartGuidanceDetail{
				ManagedPACActive:   true,
				ManagedPACServices: []string{"Wi-Fi"},
				UpstreamListWarnings: []gateway.UpstreamListWarningDetail{{
					Line:       2,
					Text:       "https://*.bad.example.test",
					Diagnostic: "wildcards require a Host Selector",
				}},
			},
		}
		hooks.Started(result)
		return result, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"seamless-cors running",
		"managed-pac: active",
		"managed-pac-services: Wi-Fi",
		"warning: upstream-list line 2: https://*.bad.example.test: wildcards require a Host Selector",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("start output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStartCommandShortensHomePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var out bytes.Buffer
	renderStartResult(&out, gateway.StartResult{
		Kind: gateway.StartResultStarted,
		Guidance: &gateway.StartGuidanceDetail{
			UpstreamListPath: filepath.Join(home, ".seamless-cors", "upstreams.txt"),
		},
	})

	wantUpstreams := "upstream-list: " + filepath.Join("~", ".seamless-cors", "upstreams.txt")
	for _, want := range []string{wantUpstreams} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("start output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), home) {
		t.Fatalf("start output contains home path %q:\n%s", home, out.String())
	}
}

func TestHomeRelativePathLeavesPathsOutsideHomeUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	outside := filepath.Join(filepath.Dir(home), filepath.Base(home)+"-other", "config.yaml")

	if got := homeRelativePath(outside); got != outside {
		t.Fatalf("homeRelativePath(%q) = %q", outside, got)
	}
}

func TestServeCommandRendersReadiness(t *testing.T) {
	var out bytes.Buffer
	err := serveWithContext(context.Background(), &out, func(_ context.Context, ready func()) error {
		ready()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "gateway owner running") {
		t.Fatalf("serve output = %q", out.String())
	}
}

func TestStopCommandRendersCleanupFailure(t *testing.T) {
	var out bytes.Buffer
	err := stopGatewayWithCommand(context.Background(), &out, func(context.Context) (gateway.StopResult, error) {
		return gateway.StopResult{
			Kind: gateway.StopResultCleanupFailed,
			CleanupFailures: []gateway.CleanupFailureDetail{{
				Subject:    gateway.CleanupSubjectManagedPAC,
				Diagnostic: "cleanup denied",
			}},
		}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "cleanup denied") {
		t.Fatalf("stop error = %v", err)
	}
	if !strings.Contains(out.String(), "stop cleanup failed") {
		t.Fatalf("stop output = %q", out.String())
	}
}

func TestStatusCommandRendersStaleCacheGuidance(t *testing.T) {
	var out bytes.Buffer
	err := statusWithCommand(context.Background(), &out, func(context.Context) (gateway.StatusResult, error) {
		return gateway.StatusResult{Kind: gateway.GatewayStatusStaleCache}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stale Gateway State Cache detected") {
		t.Fatalf("status output = %q", out.String())
	}
}

func TestStatusRendersCurrentUpstreamListWarnings(t *testing.T) {
	var out bytes.Buffer
	renderStatus(&out, gateway.StatusResult{
		Kind: gateway.GatewayStatusRunning,
		Runtime: &gateway.RuntimeStatusDetail{
			UpstreamListWarnings: []gateway.UpstreamListWarningDetail{{
				Line:       4,
				Text:       "bad/origin",
				Diagnostic: "Host Selector must not include a scheme, port, or path",
			}},
		},
	})
	if !strings.Contains(out.String(), "warning: upstream-list line 4: bad/origin: Host Selector") {
		t.Fatalf("status output = %q", out.String())
	}
}

func TestStatusRendersUnmetHTTPSIntent(t *testing.T) {
	var out bytes.Buffer
	renderStatus(&out, gateway.StatusResult{
		Kind: gateway.GatewayStatusRunning,
		Runtime: &gateway.RuntimeStatusDetail{
			HTTPSReadiness: gateway.HTTPSReadinessNotReady,
			HTTPSIntent:    true,
			HTTPSWarnings: []gateway.HTTPSWarningDetail{{
				Kind:       gateway.HTTPSWarningUnmetIntent,
				Diagnostic: "HTTPS was requested but the Installed User CA is missing.",
				Action:     "Run `seamless-cors install`.",
			}},
		},
	})
	status := out.String()
	for _, expected := range []string{
		"https: inactive",
		"warning: HTTPS was requested but the Installed User CA is missing.",
		"action: Run `seamless-cors install`.",
	} {
		if !strings.Contains(status, expected) {
			t.Fatalf("status output = %q, want %q", status, expected)
		}
	}
}

func TestStatusRendersOwnerEndingGuidance(t *testing.T) {
	var out bytes.Buffer
	renderStatus(&out, gateway.StatusResult{
		Kind:  gateway.GatewayStatusEnding,
		Owner: &gateway.OwnerStatusDetail{RouterListen: "127.0.0.1:1234"},
	})
	if !strings.Contains(out.String(), "seamless-cors status: owner ending") {
		t.Fatalf("status output = %q", out.String())
	}
	if !strings.Contains(out.String(), "retry-stop: run `seamless-cors stop`") {
		t.Fatalf("status output = %q", out.String())
	}
	if strings.Contains(out.String(), "runtime-proxy-endpoint:") {
		t.Fatalf("status output includes runtime detail = %q", out.String())
	}
}

func TestInstallAndUninstallCommandsRenderResults(t *testing.T) {
	var out bytes.Buffer
	if err := installCAWithCommand(context.Background(), &out, func(context.Context) (gateway.InstallResult, error) {
		return gateway.InstallResult{Kind: gateway.InstallResultAlreadyUsable}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already usable") || !strings.Contains(out.String(), "https-readiness: ready") {
		t.Fatalf("install output = %q", out.String())
	}
	out.Reset()
	if err := uninstallCAWithCommand(context.Background(), strings.NewReader(""), &out, func(context.Context, gateway.UninstallRequest) (gateway.UninstallResult, error) {
		return gateway.UninstallResult{Kind: gateway.UninstallResultAlreadyAbsent}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already absent") {
		t.Fatalf("uninstall output = %q", out.String())
	}
}

func TestUninstallConfirmsOnlyWhenHTTPSIsActive(t *testing.T) {
	var out bytes.Buffer
	var calls []gateway.UninstallRequest
	command := func(_ context.Context, request gateway.UninstallRequest) (gateway.UninstallResult, error) {
		calls = append(calls, request)
		if request.ConsentFingerprint == "" {
			return gateway.UninstallResult{
				Kind:               gateway.UninstallResultConsentRequired,
				ConsentFingerprint: "current-state",
			}, nil
		}
		return gateway.UninstallResult{Kind: gateway.UninstallResultUninstalled}, nil
	}

	if err := uninstallCAWithCommand(context.Background(), strings.NewReader("yes\n"), &out, command); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1].ConsentFingerprint != "current-state" {
		t.Fatalf("uninstall requests = %#v", calls)
	}
	if !strings.Contains(out.String(), "HTTPS interception is active") ||
		!strings.Contains(out.String(), "Installed User CA uninstalled") {
		t.Fatalf("uninstall output = %q", out.String())
	}
}

func TestUninstallReportsPartialPACRefreshFailure(t *testing.T) {
	var out bytes.Buffer
	err := uninstallCAWithCommand(context.Background(), strings.NewReader(""), &out, func(context.Context, gateway.UninstallRequest) (gateway.UninstallResult, error) {
		return gateway.UninstallResult{
			Kind: gateway.UninstallResultPACRefreshFailed,
			Warnings: []gateway.HTTPSWarningDetail{{
				Kind:       gateway.HTTPSWarningPACRefreshFailed,
				Diagnostic: "HTTPS routing refresh failed.",
				Action:     "Retry uninstall.",
			}},
		}, nil
	})
	if err == nil {
		t.Fatal("partial PAC refresh failure should return a command error")
	}
	for _, expected := range []string{
		"Installed User CA uninstalled, but Managed PAC refresh failed.",
		"warning: HTTPS routing refresh failed.",
		"action: Retry uninstall.",
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("uninstall output = %q, want %q", out.String(), expected)
		}
	}
}

func TestLiveHTTPSWarningRendererPrintsOnlyAddedOrChangedWarnings(t *testing.T) {
	var out bytes.Buffer
	renderer := &liveHTTPSWarningRenderer{stdout: &out}
	initial := gateway.HTTPSWarningDetail{
		Kind:       gateway.HTTPSWarningRenewalRecommended,
		Diagnostic: "UserCA expires soon.",
		Action:     "Run install.",
	}
	renderer.RenderSnapshot([]gateway.HTTPSWarningDetail{initial})
	renderer.RenderSnapshot([]gateway.HTTPSWarningDetail{initial})
	changed := initial
	changed.Diagnostic = "UserCA expires tomorrow."
	renderer.RenderSnapshot([]gateway.HTTPSWarningDetail{changed})
	renderer.RenderSnapshot(nil)

	if got := strings.Count(out.String(), "warning:"); got != 2 {
		t.Fatalf("rendered warning count = %d, output %q", got, out.String())
	}
	if !strings.Contains(out.String(), initial.Diagnostic) ||
		!strings.Contains(out.String(), changed.Diagnostic) {
		t.Fatalf("renderer output = %q", out.String())
	}
}

type testContextKey struct{}

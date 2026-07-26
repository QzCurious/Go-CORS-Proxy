package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"seamless-cors/internal/gateway"
	"seamless-cors/internal/managedpac"
	"seamless-cors/internal/userca"
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
				DomainListWarnings: []gateway.DomainListWarningDetail{{
					Line:       2,
					Text:       "https://*.bad.example.test",
					Diagnostic: "wildcards require hostname shorthand",
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
		"warning: domain-list line 2: https://*.bad.example.test: wildcards require hostname shorthand",
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
			ConfigPath:     filepath.Join(home, ".seamless-cors", "config.yaml"),
			DomainListPath: filepath.Join(home, ".seamless-cors", "domains.txt"),
		},
	})

	wantConfig := "config: " + filepath.Join("~", ".seamless-cors", "config.yaml")
	wantDomains := "domain-list: " + filepath.Join("~", ".seamless-cors", "domains.txt")
	for _, want := range []string{wantConfig, wantDomains} {
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

func TestStartCommandRendersTrustApprovalDenial(t *testing.T) {
	var out bytes.Buffer
	err := startWithContextAndInput(context.Background(), nil, &out, func(ctx context.Context, hooks gateway.StartHooks) (gateway.StartResult, error) {
		result := gateway.StartResult{Kind: gateway.StartResultPlatformApprovalDenied}
		hooks.Started(result)
		return result, userca.ErrApprovalDenied
	})
	if !errors.Is(err, userca.ErrApprovalDenied) {
		t.Fatalf("start error = %v", err)
	}
	if !strings.Contains(out.String(), "Certificate trust was not approved.") {
		t.Fatalf("start output = %q", out.String())
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

func TestStatusRendersCurrentDomainListWarnings(t *testing.T) {
	var out bytes.Buffer
	renderStatus(&out, gateway.StatusResult{
		Kind: gateway.GatewayStatusRunning,
		Runtime: &gateway.RuntimeStatusDetail{
			DomainListWarnings: []gateway.DomainListWarningDetail{{
				Line:       4,
				Text:       "bad/origin",
				Diagnostic: "host shorthand must not include scheme, port, path, or IPv6",
			}},
		},
	})
	if !strings.Contains(out.String(), "warning: domain-list line 4: bad/origin: host shorthand") {
		t.Fatalf("status output = %q", out.String())
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
	if !strings.Contains(out.String(), "already usable") {
		t.Fatalf("install output = %q", out.String())
	}
	out.Reset()
	if err := uninstallCAWithCommand(context.Background(), &out, func(context.Context) (gateway.UninstallResult, error) {
		return gateway.UninstallResult{Kind: gateway.UninstallResultAlreadyAbsent}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already absent") {
		t.Fatalf("uninstall output = %q", out.String())
	}
}

type testContextKey struct{}

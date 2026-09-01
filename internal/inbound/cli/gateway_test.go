package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/gateway"
)

func TestSecondForegroundSignalForcesExit(t *testing.T) {
	signals := make(chan os.Signal, 2)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	forced := make(chan int, 1)
	go superviseForegroundSignals(signals, done, cancel, func(code int) { forced <- code })

	signals <- os.Interrupt
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("first signal did not request graceful stop")
	}
	signals <- os.Interrupt
	select {
	case code := <-forced:
		if code != 130 {
			t.Fatalf("forced exit code = %d, want 130", code)
		}
	case <-time.After(time.Second):
		t.Fatal("second signal did not force exit")
	}
}

func TestStartGuidanceShowsSelectedAndExcludedManagedPACServices(t *testing.T) {
	var out bytes.Buffer
	renderStartResult(&out, gateway.Started{Guidance: gateway.StartGuidance{
		ManagedPAC: gateway.ManagedPACStartDetail{
			CurrentPACState: []gateway.ManagedPACServiceState{
				{ServiceName: "Ethernet", Ownership: gateway.PACOwnershipEmpty, Manageable: true},
				{ServiceName: "VPN", Ownership: gateway.PACOwnershipUnknown},
				{ServiceName: "Wi-Fi", URL: "http://corp.example/proxy.pac", Enabled: true, Ownership: gateway.PACOwnershipForeign},
			},
			ObservationIssues: []gateway.ManagedPACObservationIssue{{ServiceName: "VPN", Diagnostic: "PAC query failed"}},
			ServiceSet:        []string{"Ethernet"},
		},
	}})
	for _, want := range []string{
		"Services selected for automatic proxy management:\n  - Ethernet",
		"Network services left unchanged:",
		"VPN: proxy settings could not be read",
		"managed-pac-observation-issue: VPN: PAC query failed",
		"Wi-Fi: another PAC configuration is present",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("start guidance missing %q:\n%s", want, out.String())
		}
	}
}

func TestStopCommandDisclosesManagedPACObservationIssueOnSuccess(t *testing.T) {
	var out bytes.Buffer
	err := stopGatewayWithCommand(context.Background(), &out, func(context.Context) (gateway.StopResult, error) {
		return gateway.StopResult{
			Kind: gateway.StopResultStopped,
			ManagedPACObservationIssues: []gateway.ManagedPACObservationIssue{{
				ServiceName: "VPN",
				Diagnostic:  "PAC query failed",
			}},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "managed-pac-observation-issue: VPN: PAC query failed") {
		t.Fatalf("stop output = %q", out.String())
	}
}

func TestStartCommandRendersSurfaceNeutralResult(t *testing.T) {
	var out bytes.Buffer
	ctx := context.WithValue(context.Background(), testContextKey{}, "start")
	err := startWithContextAndInput(ctx, bytes.NewBufferString("yes"), &out, func(got context.Context, hooks gateway.StartHooks) (gateway.StartResult, error) {
		if got.Value(testContextKey{}) != "start" {
			t.Fatal("start context was not forwarded")
		}
		result := gateway.Started{Guidance: gateway.StartGuidance{
			ManagedPAC: gateway.ManagedPACStartDetail{ServiceSet: []string{"Wi-Fi"}},
			UpstreamLists: []gateway.UpstreamListSourceDetail{{
				Kind: gateway.UpstreamListSourceGlobal,
				Path: "/config/seamless-cors/upstreams.txt",
				Warnings: []gateway.UpstreamListWarningDetail{{
					Source:     gateway.UpstreamListSourceGlobal,
					Path:       "/config/seamless-cors/upstreams.txt",
					Line:       2,
					Text:       "https://*.bad.example.test",
					Diagnostic: "wildcards require a Host Selector",
				}},
			}},
		}}
		hooks.Started(result)
		return result, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"seamless-cors is running.",
		"Services selected for automatic proxy management:\n  - Wi-Fi",
		"warning: global /config/seamless-cors/upstreams.txt:2: https://*.bad.example.test: wildcards require a Host Selector",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("start output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStartCommandRendersUpstreamWarningsAfterHumanSummary(t *testing.T) {
	var out bytes.Buffer
	renderStartResult(&out, gateway.Started{Guidance: gateway.StartGuidance{
		ManagedPAC: gateway.ManagedPACStartDetail{
			ServiceSet: []string{"Wi-Fi"},
		},
		UpstreamLists: []gateway.UpstreamListSourceDetail{{
			Kind: gateway.UpstreamListSourceGlobal,
			Path: "/config/seamless-cors/upstreams.txt",
			Warnings: []gateway.UpstreamListWarningDetail{{
				Source:     gateway.UpstreamListSourceGlobal,
				Path:       "/config/seamless-cors/upstreams.txt",
				Line:       5,
				Text:       "localhost:12094",
				Diagnostic: "invalid selector",
			}},
		}},
	}})

	summaryEnd := strings.Index(out.String(), "  - Wi-Fi")
	warning := strings.Index(out.String(), "warning: global")
	if summaryEnd < 0 || warning < summaryEnd {
		t.Fatalf("upstream warning interrupted start summary:\n%s", out.String())
	}
}

func TestStartResultRendersIndependentUpstreamListIssues(t *testing.T) {
	var out bytes.Buffer
	renderStartResult(&out, gateway.Started{Guidance: gateway.StartGuidance{
		UpstreamLists: []gateway.UpstreamListSourceDetail{{
			Kind:            gateway.UpstreamListSourceGlobal,
			Path:            "/config/seamless-cors/upstreams.txt",
			FileSyncIssue:   &gateway.FileSyncIssue{Kind: gateway.FileSyncIssueObservationStopped, Cause: "watcher unavailable"},
			ProjectionIssue: &gateway.UpstreamListProjectionIssue{Cause: "content must be UTF-8"},
		}},
	}})

	for _, want := range []string{
		"global /config/seamless-cors/upstreams.txt observation stopped: watcher unavailable",
		"repair the cause and restart seamless-cors",
		"global /config/seamless-cors/upstreams.txt contents rejected: content must be UTF-8",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("start output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRecoverableFileSyncIssueRendersRepairAction(t *testing.T) {
	var out bytes.Buffer
	renderFileSyncIssue(&out, gateway.UpstreamListSourceGlobal, "/config/seamless-cors/upstreams.txt", &gateway.FileSyncIssue{
		Kind:  gateway.FileSyncIssueFileUnreadable,
		Cause: "file missing",
	})

	for _, want := range []string{
		"global /config/seamless-cors/upstreams.txt unreadable: file missing",
		"restore the upstream-list file; observation will resume automatically",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("file issue output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStartCommandFailsWhenNoManagedPACServiceIsManageable(t *testing.T) {
	var out bytes.Buffer
	result := gateway.StartNoManageablePACServices{
		UpstreamListCreationWarning: &gateway.UpstreamListCreationWarningDetail{Cause: "create denied"},
	}
	err := startWithContextAndInput(context.Background(), nil, &out, func(_ context.Context, hooks gateway.StartHooks) (gateway.StartResult, error) {
		hooks.Started(result)
		return result, nil
	})

	if err == nil || !strings.Contains(err.Error(), "no manageable PAC services") {
		t.Fatalf("start error = %v", err)
	}
	if !strings.Contains(out.String(), "could not start") {
		t.Fatalf("start output = %q", out.String())
	}
	if !strings.Contains(out.String(), "warning: upstream-list creation failed: create denied") {
		t.Fatalf("start output = %q", out.String())
	}
}

func TestStartCommandReportsManagedPACSetWarnings(t *testing.T) {
	var out bytes.Buffer
	result := gateway.StartManagedPACSetFailed{
		Diagnostic: "managed PAC Set updated no services",
		Warnings: []gateway.ManagedPACWarningDetail{{
			Kind:        gateway.ManagedPACWarningUpdateFailed,
			ServiceName: "Wi-Fi",
			Diagnostic:  "PAC write denied",
		}},
	}
	err := startWithContextAndInput(context.Background(), nil, &out, func(_ context.Context, hooks gateway.StartHooks) (gateway.StartResult, error) {
		hooks.Started(result)
		return result, nil
	})

	if err == nil || !strings.Contains(err.Error(), result.Diagnostic) {
		t.Fatalf("start error = %v", err)
	}
	if !strings.Contains(out.String(), "managed-pac-warning: Wi-Fi: PAC write denied") {
		t.Fatalf("start output = %q", out.String())
	}
}

func TestStartCommandRendersHumanSummary(t *testing.T) {
	var out bytes.Buffer
	renderStartResult(&out, gateway.Started{Guidance: gateway.StartGuidance{
		UpstreamLists: []gateway.UpstreamListSourceDetail{
			{Kind: gateway.UpstreamListSourceGlobal, Path: "/config/seamless-cors/upstreams.txt"},
			{Kind: gateway.UpstreamListSourceDirectory, Path: "/project/upstreams.txt"},
		},
		ManagedPAC: gateway.ManagedPACStartDetail{
			ServiceSet: []string{"Ethernet", "Wi-Fi"},
		},
		Traffic: gateway.TrafficStatusDetail{
			ProjectionCurrent: true,
			HTTPCORS:          gateway.TrafficFeatureInactive,
			HTTPSCORS:         gateway.TrafficFeatureInactive,
			HTTPSFacade:       gateway.TrafficFeatureInactive,
		},
		InstalledCA: gateway.InstalledCAStatusDetail{Health: gateway.CAHealthUsable},
	}})

	want := "" +
		"seamless-cors is running.\n" +
		"Upstream lists:\n" +
		"  Global: /config/seamless-cors/upstreams.txt\n" +
		"  Directory: /project/upstreams.txt\n" +
		"traffic-routing-ready: false\n" +
		"traffic-projection-current: true\n" +
		"http-cors: inactive\n" +
		"https-cors: inactive\n" +
		"https-facade: inactive\n" +
		"Services selected for automatic proxy management:\n" +
		"  - Ethernet\n" +
		"  - Wi-Fi\n"
	if out.String() != want {
		t.Fatalf("start output:\n%s\nwant:\n%s", out.String(), want)
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

func TestStopCommandRendersCleanupFailure(t *testing.T) {
	var out bytes.Buffer
	err := stopGatewayWithCommand(context.Background(), &out, func(context.Context) (gateway.StopResult, error) {
		return gateway.StopResult{
			Kind: gateway.StopResultCleanupFailed,
			CleanupFailures: []gateway.CleanupFailure{{
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
		return gateway.StatusResult{
			Kind:         gateway.StatusResultReported,
			StatusReport: gateway.StatusReport{State: gateway.GatewayStatusStaleCache},
		}, nil
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
		Kind: gateway.StatusResultReported,
		StatusReport: gateway.StatusReport{
			State: gateway.GatewayStatusRunning,
			Runtime: &gateway.RuntimeStatusDetail{
				UpstreamLists: []gateway.UpstreamListSourceDetail{{
					Kind: gateway.UpstreamListSourceDirectory,
					Path: "/project/upstreams.txt",
					Warnings: []gateway.UpstreamListWarningDetail{{
						Source:     gateway.UpstreamListSourceDirectory,
						Path:       "/project/upstreams.txt",
						Line:       4,
						Text:       "bad/origin",
						Diagnostic: "Host Selector must not include a scheme, port, or path",
					}},
				}},
			},
		},
	})
	if !strings.Contains(out.String(), "warning: directory /project/upstreams.txt:4: bad/origin: Host Selector") {
		t.Fatalf("status output = %q", out.String())
	}
}

func TestStatusRendersUserCARenewalDueFact(t *testing.T) {
	var out bytes.Buffer
	expires := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renderStatus(&out, gateway.StatusResult{
		Kind: gateway.StatusResultReported,
		StatusReport: gateway.StatusReport{
			State: gateway.GatewayStatusRouterOnly,
			InstalledCA: gateway.InstalledCAStatusDetail{
				Health:     gateway.CAHealthUsable,
				Expires:    expires,
				RenewalDue: true,
			},
		},
	})
	for _, want := range []string{
		"installed-ca: usable",
		"installed-ca-expires: 2026-10-01",
		"installed-ca-renewal: due",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStatusRendersBlockedHTTPSCORSWithInstallGuidance(t *testing.T) {
	var out bytes.Buffer
	renderStatus(&out, gateway.StatusResult{
		Kind: gateway.StatusResultReported,
		StatusReport: gateway.StatusReport{
			State:       gateway.GatewayStatusRunning,
			InstalledCA: gateway.InstalledCAStatusDetail{Health: gateway.CAHealthNotUsable},
			Runtime: &gateway.RuntimeStatusDetail{
				Traffic: gateway.TrafficStatusDetail{
					ProjectionCurrent: true,
					HTTPCORS:          gateway.TrafficFeatureInactive,
					HTTPSCORS:         gateway.TrafficFeatureBlocked,
					HTTPSFacade:       gateway.TrafficFeatureInactive,
				},
			},
		},
	})
	status := out.String()
	for _, expected := range []string{
		"https-cors: blocked",
		"action: Run `seamless-cors install` to install or repair the User CA.",
	} {
		if !strings.Contains(status, expected) {
			t.Fatalf("status output = %q, want %q", status, expected)
		}
	}
}

func TestStatusRendersOwnerEndingGuidance(t *testing.T) {
	var out bytes.Buffer
	renderStatus(&out, gateway.StatusResult{
		Kind: gateway.StatusResultReported,
		StatusReport: gateway.StatusReport{
			State: gateway.GatewayStatusEnding,
			Owner: &gateway.OwnerStatusDetail{RouterListen: "127.0.0.1:1234"},
		},
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
		return gateway.InstallResult{Kind: gateway.InstallResultInstalled}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "User CA is installed.") || strings.Contains(out.String(), "https-readiness:") {
		t.Fatalf("install output = %q", out.String())
	}
	out.Reset()
	if err := uninstallCAWithCommand(context.Background(), strings.NewReader(""), &out, func(context.Context, gateway.UninstallRequest) (gateway.UninstallResult, error) {
		return gateway.UninstallResult{Kind: gateway.UninstallResultUninstalled}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "User CA is uninstalled.") {
		t.Fatalf("uninstall output = %q", out.String())
	}
}

func TestStartReportsFailFastCAAdmission(t *testing.T) {
	var out bytes.Buffer
	err := startWithContextAndInput(context.Background(), nil, &out, func(context.Context, gateway.StartHooks) (gateway.StartResult, error) {
		return gateway.StartAlreadyMutating{}, nil
	})

	if err == nil || !strings.Contains(err.Error(), "CA operation in progress") {
		t.Fatalf("start error = %v", err)
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
		!strings.Contains(out.String(), "User CA is uninstalled.") {
		t.Fatalf("uninstall output = %q", out.String())
	}
}

func TestStartRendersManagedPACWarningsSeparately(t *testing.T) {
	var out bytes.Buffer
	renderStartResult(&out, gateway.Started{Guidance: gateway.StartGuidance{
		ManagedPAC: gateway.ManagedPACStartDetail{Warnings: []gateway.ManagedPACWarningDetail{{
			Kind:        gateway.ManagedPACWarningDrift,
			ServiceName: "Wi-Fi",
			Diagnostic:  "foreign PAC state is active",
		}}},
	}})
	if !strings.Contains(out.String(), "managed-pac-warning: Wi-Fi: foreign PAC state is active") {
		t.Fatalf("start output = %q", out.String())
	}
}

func TestStartRendersManagedPACObservationIssues(t *testing.T) {
	var out bytes.Buffer
	renderStartResult(&out, gateway.Started{Guidance: gateway.StartGuidance{
		ManagedPAC: gateway.ManagedPACStartDetail{ObservationIssues: []gateway.ManagedPACObservationIssue{{ServiceName: "VPN", Diagnostic: "PAC query failed"}}},
	}})
	if !strings.Contains(out.String(), "managed-pac-observation-issue: VPN: PAC query failed") {
		t.Fatalf("start output = %q", out.String())
	}
}

func TestStatusRendersUserCAAssessmentIssueWithoutInstallGuidance(t *testing.T) {
	var out bytes.Buffer
	renderStatus(&out, gateway.StatusResult{
		Kind: gateway.StatusResultReported,
		StatusReport: gateway.StatusReport{
			State:       gateway.GatewayStatusRunning,
			InstalledCA: gateway.InstalledCAStatusDetail{Health: gateway.CAHealthNotUsable},
			UserCAAssessmentIssue: &gateway.UserCAAssessmentIssue{
				Cause: "trust store unavailable",
			},
		},
	})

	if !strings.Contains(out.String(), "userca-assessment-issue: trust store unavailable") {
		t.Fatalf("status output = %q", out.String())
	}
	if strings.Contains(out.String(), "seamless-cors install") {
		t.Fatalf("assessment issue claimed install would repair it: %q", out.String())
	}
}

type testContextKey struct{}

package cli

import (
	"bytes"
	"context"
	"os"
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
			t.Fatalf("code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("second signal did not force exit")
	}
}

func TestStartRendersEverySystemPACServiceAndDeliveryIssue(t *testing.T) {
	var out bytes.Buffer
	renderStartResult(&out, gateway.Started{Guidance: gateway.StartGuidance{SystemPAC: gateway.SystemPACReport{
		Generation: 7,
		Services: []gateway.SystemPACServiceState{
			{Name: "Ethernet", Ownership: "owned", Enabled: true},
			{Name: "Wi-Fi", Ownership: "foreign", Enabled: true},
		},
		Issues: []gateway.SystemPACIssue{{Kind: gateway.SystemPACIssueMutation, ServiceName: "Ethernet", Cause: "write denied"}},
	}}})
	for _, want := range []string{"system-pac-delivery-generation: 7", "Ethernet: owned: enabled", "Wi-Fi: foreign: enabled", "mutation: Ethernet: write denied"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStopCleanupUncertaintyIsProminentButSuccessful(t *testing.T) {
	var out bytes.Buffer
	err := stopGatewayWithCommand(context.Background(), &out, func(context.Context) (gateway.StopResult, error) {
		return gateway.StopResult{Kind: gateway.StopResultStopped, CleanupFulfillment: gateway.CommandUnfulfilled,
			SystemPACCleanup: gateway.SystemPACReport{Issues: []gateway.SystemPACIssue{{Kind: gateway.SystemPACIssueVerification, ServiceName: "VPN", Cause: "verification uncertain"}}},
			CleanupFailures:  []gateway.CleanupFailure{{Subject: gateway.CleanupSubjectSystemPAC, Diagnostic: "verification uncertain"}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cleanup is incomplete", "owned PAC or Gateway state may remain", "verification uncertain", "system-pac-cleanup-issue: verification: VPN", "rerun `seamless-cors stop`"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStatusLabelsFreshAndHistoricalSystemPACReports(t *testing.T) {
	var out bytes.Buffer
	historical := gateway.SystemPACReport{Generation: 3, Issues: []gateway.SystemPACIssue{{Kind: gateway.SystemPACIssueMutation, Cause: "old failure"}}}
	renderStatus(&out, gateway.StatusResult{StatusReport: gateway.StatusReport{State: gateway.GatewayStatusRunning, Runtime: &gateway.RuntimeStatusDetail{
		LatestSystemPACDelivery: &historical,
	}, SystemPAC: gateway.SystemPACReport{Services: []gateway.SystemPACServiceState{{Name: "Wi-Fi", Ownership: "owned", Enabled: true}}}, InstalledCA: gateway.InstalledCAStatusDetail{Health: gateway.CAHealthUsable}}})
	for _, want := range []string{"system-pac-current-service", "system-pac-historical-delivery-generation: 3", "old failure"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRecoverableFileSyncIssueRendersRepairAction(t *testing.T) {
	var out bytes.Buffer
	renderFileSyncIssue(&out, gateway.UpstreamListSourceGlobal, "/config/upstreams.txt", &gateway.FileSyncIssue{Kind: gateway.FileSyncIssueFileUnreadable, Cause: "file missing"})
	for _, want := range []string{"unreadable: file missing", "observation will resume automatically"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q: %s", want, out.String())
		}
	}
}

func TestStatusRendersBlockedHTTPSCORSWithInstallGuidance(t *testing.T) {
	var out bytes.Buffer
	renderStatus(&out, gateway.StatusResult{StatusReport: gateway.StatusReport{
		State:       gateway.GatewayStatusRunning,
		InstalledCA: gateway.InstalledCAStatusDetail{Health: gateway.CAHealthNotUsable},
		Runtime:     &gateway.RuntimeStatusDetail{Traffic: gateway.TrafficStatusDetail{HTTPSCORS: gateway.TrafficFeatureBlocked}},
	}})
	for _, want := range []string{"https-cors: blocked", "Run `seamless-cors install`"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q: %s", want, out.String())
		}
	}
}

func TestInstallAndUninstallCommandsRenderResults(t *testing.T) {
	var out bytes.Buffer
	if err := installCAWithCommand(context.Background(), &out, func(context.Context) (gateway.InstallResult, error) {
		return gateway.InstallResult{Kind: gateway.InstallResultInstalled}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "User CA is installed.") {
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

func TestUninstallConfirmsOnlyWhenHTTPSIsActive(t *testing.T) {
	var out bytes.Buffer
	var calls []gateway.UninstallRequest
	command := func(_ context.Context, request gateway.UninstallRequest) (gateway.UninstallResult, error) {
		calls = append(calls, request)
		if request.ConsentFingerprint == "" {
			return gateway.UninstallResult{Kind: gateway.UninstallResultConsentRequired, ConsentFingerprint: "current-state"}, nil
		}
		return gateway.UninstallResult{Kind: gateway.UninstallResultUninstalled}, nil
	}
	if err := uninstallCAWithCommand(context.Background(), strings.NewReader("yes\n"), &out, command); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1].ConsentFingerprint != "current-state" {
		t.Fatalf("calls = %#v", calls)
	}
	if !strings.Contains(out.String(), "HTTPS interception is active") {
		t.Fatalf("output = %q", out.String())
	}
}

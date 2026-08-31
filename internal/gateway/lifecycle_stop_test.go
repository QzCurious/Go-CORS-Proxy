package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

func TestExecuteStartRejectsStartWhileStopCleanupIsRunning(t *testing.T) {
	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	settings := &blockingCleanupSettings{
		cleanupEntered: cleanupEntered,
		releaseCleanup: releaseCleanup,
	}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}

	stopDone := make(chan StopResult, 1)
	go func() {
		result, _ := lifecycle.Stop(context.Background())
		stopDone <- result
	}()
	<-cleanupEntered

	start, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if start.Kind() != StartResultStopCancelled {
		t.Fatalf("start kind = %s, want %s", start.Kind(), StartResultStopCancelled)
	}

	close(releaseCleanup)
	if result := <-stopDone; result.Kind != StopResultStopped {
		t.Fatalf("stop kind = %s, want %s", result.Kind, StopResultStopped)
	}
}

func TestRetryableStopFailureLeavesOwnerEnding(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	settings := &lifecycleTestSystemSettings{
		services: []managedpac.Service{{
			Name:      "Wi-Fi",
			URL:       "http://127.0.0.1/seamless-cors.pac",
			Enabled:   true,
			Ownership: managedpac.OwnershipOwned,
		}},
		clearErr: errors.New("cleanup denied"),
	}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, coord, "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	cache := stateCache{HTTPRouterListen: "127.0.0.1:1", Token: "token"}
	if err := coord.Claim(cache); err != nil {
		t.Fatal(err)
	}
	lifecycle.SetOwnerCache(cache)

	stopped, err := lifecycle.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Kind != StopResultCleanupFailed {
		t.Fatalf("stop kind = %s, want %s", stopped.Kind, StopResultCleanupFailed)
	}
	if settings.cleanupCalls != 0 || settings.uninstallCalls != 1 {
		t.Fatalf("cleanup calls = %d, uninstall calls = %d", settings.cleanupCalls, settings.uninstallCalls)
	}
	if coord.Exists() {
		t.Fatal("cleanup failure preserved Gateway State Cache")
	}

	status, err := lifecycle.Status(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != GatewayStatusEnding {
		t.Fatalf("status state = %s, want %s", status.State, GatewayStatusEnding)
	}
	if status.Owner == nil || status.Owner.RouterListen != "127.0.0.1:1" {
		t.Fatalf("status owner = %#v, want router detail", status.Owner)
	}
	if status.Runtime != nil {
		t.Fatalf("status runtime = %#v, want no detached runtime detail", status.Runtime)
	}

	start, err := lifecycle.ExecuteStart(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if start.Kind() != StartResultStopCancelled {
		t.Fatalf("start kind = %s, want %s", start.Kind(), StartResultStopCancelled)
	}
}

func TestStopSucceedsAndDisclosesManagedPACObservationIssue(t *testing.T) {
	settings := &lifecycleTestSystemSettings{
		cleanupResult: managedpac.NewCleanupResult([]managedpac.ObservationIssue{{
			ServiceName: "VPN",
			Diagnostic:  "PAC query failed",
		}}),
	}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := lifecycle.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StopResultStopped || result.Fulfillment() != CommandFulfilled {
		t.Fatalf("stop result = %#v", result)
	}
	if len(result.ManagedPACObservationIssues) != 1 || result.ManagedPACObservationIssues[0].ServiceName != "VPN" {
		t.Fatalf("observation issues = %#v", result.ManagedPACObservationIssues)
	}
}

type blockingCleanupSettings struct {
	cleanupEntered chan<- struct{}
	releaseCleanup <-chan struct{}
}

func (*blockingCleanupSettings) Inspect(context.Context) (managedpac.Snapshot, error) {
	return managedpac.Snapshot{}, nil
}

func (*blockingCleanupSettings) Install(context.Context, []string, string) (managedpac.InstallResult, error) {
	return managedpac.InstallResult{}, nil
}

func (*blockingCleanupSettings) Deliver() {}

func (*blockingCleanupSettings) RoutingReady(context.Context, string) (bool, []managedpac.ObservationIssue, error) {
	return false, nil, nil
}

func (*blockingCleanupSettings) ReconciliationResults() <-chan managedpac.ReconciliationResult {
	return make(chan managedpac.ReconciliationResult)
}

func (f *blockingCleanupSettings) CleanupActiveState(ctx context.Context) (managedpac.CleanupResult, error) {
	return f.Uninstall(ctx)
}

func (f *blockingCleanupSettings) Uninstall(context.Context) (managedpac.CleanupResult, error) {
	close(f.cleanupEntered)
	<-f.releaseCleanup
	return managedpac.CleanupResult{}, nil
}

package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

func TestStopCancelsSetBeforeWaitingForStart(t *testing.T) {
	settings := &stopCancelableManagedPAC{
		setEntered: make(chan struct{}),
		closed:     make(chan struct{}),
	}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(globalPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle.globalUpstreamListPath = globalPath

	startDone := make(chan StartResult, 1)
	go func() {
		result, _ := lifecycle.ExecuteStart(context.Background(), StartRequest{WorkingDirectory: t.TempDir()})
		startDone <- result
	}()
	<-settings.setEntered

	stopDone := make(chan StopResult, 1)
	go func() {
		result, _ := lifecycle.Stop(context.Background())
		stopDone <- result
	}()
	select {
	case stopped := <-stopDone:
		if stopped.Kind != StopResultStopped {
			t.Fatalf("stop kind = %s, want %s", stopped.Kind, StopResultStopped)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop waited for Start before closing Managed PAC control")
	}
	if started := <-startDone; started.Kind() != StartResultStopCancelled {
		t.Fatalf("start kind = %s, want %s", started.Kind(), StartResultStopCancelled)
	}
}

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
		services: []managedPACTestService{{
			ServiceName: "Wi-Fi",
			URL:         "http://127.0.0.1/seamless-cors.pac",
			Enabled:     true,
			Ownership:   managedpac.OwnershipOwned,
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
	if settings.cleanupCalls != 1 || settings.uninstallCalls != 0 {
		t.Fatalf("cleanup calls = %d, control close calls = %d", settings.cleanupCalls, settings.uninstallCalls)
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
		cleanupResult: []managedpac.ObservationIssue{{
			ServiceName: "VPN",
			Diagnostic:  "PAC query failed",
		}},
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

type stopCancelableManagedPAC struct {
	setEntered chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once
}

func (*stopCancelableManagedPAC) InspectFootprint(context.Context) (managedpac.FootprintReport, error) {
	return managedpac.FootprintReport{}, nil
}

func (f *stopCancelableManagedPAC) Begin(context.Context) (managedpac.Control, managedpac.Assessment, error) {
	return f, managedpac.Assessment{
		Services:   []managedpac.AssessedService{{ServiceName: "Wi-Fi", Ownership: managedpac.OwnershipEmpty, Manageable: true}},
		ServiceSet: []string{"Wi-Fi"},
	}, nil
}

func (f *stopCancelableManagedPAC) Deliver(string) (managedpac.ControlState, error) {
	close(f.setEntered)
	<-f.closed
	return managedpac.ControlState{}, errors.New("managed PAC control is closed")
}

func (*stopCancelableManagedPAC) Observe() (managedpac.ControlState, error) {
	return managedpac.ControlState{}, nil
}

func (*stopCancelableManagedPAC) Cleanup(context.Context) (managedpac.CleanupReport, error) {
	return managedpac.CleanupReport{}, nil
}

func (f *stopCancelableManagedPAC) Close() (managedpac.CleanupReport, error) {
	f.closeOnce.Do(func() { close(f.closed) })
	return managedpac.CleanupReport{}, nil
}

func (*blockingCleanupSettings) InspectFootprint(context.Context) (managedpac.FootprintReport, error) {
	return managedpac.FootprintReport{}, nil
}

func (f *blockingCleanupSettings) Begin(context.Context) (managedpac.Control, managedpac.Assessment, error) {
	return f, managedpac.Assessment{}, nil
}

func (*blockingCleanupSettings) Deliver(string) (managedpac.ControlState, error) {
	return managedpac.ControlState{}, nil
}

func (*blockingCleanupSettings) Observe() (managedpac.ControlState, error) {
	return managedpac.ControlState{}, nil
}

func (f *blockingCleanupSettings) Cleanup(ctx context.Context) (managedpac.CleanupReport, error) {
	return f.close()
}

func (f *blockingCleanupSettings) Close() (managedpac.CleanupReport, error) {
	return f.close()
}

func (f *blockingCleanupSettings) close() (managedpac.CleanupReport, error) {
	close(f.cleanupEntered)
	<-f.releaseCleanup
	return managedpac.CleanupReport{}, nil
}

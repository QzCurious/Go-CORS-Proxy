package gateway

import (
	"context"
	"errors"
	"testing"
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
	if start.Kind != StartResultStopCancelled {
		t.Fatalf("start kind = %s, want %s", start.Kind, StartResultStopCancelled)
	}

	close(releaseCleanup)
	if result := <-stopDone; result.Kind != StopResultStopped {
		t.Fatalf("stop kind = %s, want %s", result.Kind, StopResultStopped)
	}
}

func TestRetryableStopFailureLeavesOwnerEnding(t *testing.T) {
	settings := &lifecycleTestSystemSettings{
		states: []testPACState{{
			ServiceName: "Wi-Fi",
			PACURL:      "http://127.0.0.1/seamless-cors.pac",
			Enabled:     true,
		}},
		clearErr: errors.New("cleanup denied"),
	}
	lifecycle, err := newLifecycle(settings, emptyTestUserCA{}, newCoordinator(t.TempDir()), "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}

	stopped, err := lifecycle.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Kind != StopResultCleanupFailed {
		t.Fatalf("stop kind = %s, want %s", stopped.Kind, StopResultCleanupFailed)
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
	if start.Kind != StartResultStopCancelled {
		t.Fatalf("start kind = %s, want %s", start.Kind, StartResultStopCancelled)
	}
}

type blockingCleanupSettings struct {
	cleanupEntered chan<- struct{}
	releaseCleanup <-chan struct{}
}

func (*blockingCleanupSettings) Inspect(context.Context) (managedPACSnapshot, error) {
	return managedPACSnapshot{}, nil
}

func (*blockingCleanupSettings) Install(context.Context, []string, string) (managedPACInstallResult, error) {
	return managedPACInstallResult{}, nil
}

func (*blockingCleanupSettings) RequestReconcile(managedPACRuntimeState, string, func(managedPACReconcileResult)) {
}

func (f *blockingCleanupSettings) Uninstall(context.Context) error {
	close(f.cleanupEntered)
	<-f.releaseCleanup
	return nil
}

package gateway

import (
	"context"
	"errors"
	"testing"

	"seamless-cors/internal/managedpac"
)

func TestExecuteStartRejectsStartWhileStopCleanupIsRunning(t *testing.T) {
	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	settings := &blockingCleanupSettings{
		cleanupEntered: cleanupEntered,
		releaseCleanup: releaseCleanup,
	}
	lifecycle, err := newLifecycle(settings, emptyTestTrustStore{}, newCoordinator(t.TempDir()), "127.0.0.1:1")
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
		states: []managedpac.ServiceSnapshot{{
			ServiceName: "Wi-Fi",
			PACURL:      "http://127.0.0.1/seamless-cors.pac",
			Enabled:     true,
		}},
		clearErr: errors.New("cleanup denied"),
	}
	lifecycle, err := newLifecycle(settings, emptyTestTrustStore{}, newCoordinator(t.TempDir()), "127.0.0.1:1")
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
	if status.Kind != GatewayStatusEnding {
		t.Fatalf("status kind = %s, want %s", status.Kind, GatewayStatusEnding)
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

func (*blockingCleanupSettings) Apply(context.Context, string, []string) (managedpac.ApplyResult, error) {
	return managedpac.ApplyResult{}, nil
}

func (f *blockingCleanupSettings) Snapshot(context.Context) ([]managedpac.ServiceSnapshot, error) {
	close(f.cleanupEntered)
	<-f.releaseCleanup
	return nil, nil
}

func (*blockingCleanupSettings) ClearIfUnchanged(context.Context, []managedpac.ServiceSnapshot) error {
	return nil
}

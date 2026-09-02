package gateway

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/systempac"
)

type cleanupProbePAC struct {
	endpoint               string
	cleanupCalls           int
	reachableDuringCleanup bool
	err                    error
}

type quiescingPAC struct {
	deliverEntered chan struct{}
	releaseDeliver chan struct{}
	cleanupEntered chan struct{}
	deliverCalls   int
}

func (f *quiescingPAC) Deliver(context.Context, string) (systempac.State, error) {
	f.deliverCalls++
	close(f.deliverEntered)
	<-f.releaseDeliver
	return systempac.State{}, nil
}
func (f *quiescingPAC) Observe(context.Context, string) (systempac.State, error) {
	return systempac.State{}, nil
}
func (f *quiescingPAC) Cleanup(context.Context) ([]systempac.ServiceState, error) {
	close(f.cleanupEntered)
	return nil, nil
}

func (f *cleanupProbePAC) Deliver(context.Context, string) (systempac.State, error) {
	return systempac.State{}, nil
}
func (f *cleanupProbePAC) Observe(context.Context, string) (systempac.State, error) {
	return systempac.State{}, nil
}
func (f *cleanupProbePAC) Cleanup(context.Context) ([]systempac.ServiceState, error) {
	f.cleanupCalls++
	conn, err := net.DialTimeout("tcp", f.endpoint, time.Second)
	if err == nil {
		f.reachableDuringCleanup = true
		_ = conn.Close()
	}
	return nil, f.err
}

func TestStopCleansSystemPACWhileRuntimeServesAndRemainsFulfilledOnFailure(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents(nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runtime.ServeReady(ctx, ready) }()
	<-ready
	pac := &cleanupProbePAC{endpoint: runtime.PACListen(), err: systempac.VerificationError{ServiceName: "Wi-Fi", Cause: errors.New("verification uncertain")}}
	lifecycle, err := newLifecycle(pac, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.runtime = &activeRuntime{engine: runtime, ctx: ctx, cancel: cancel, phase: runtimePhaseRunning}

	result, err := lifecycle.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Fulfillment() != CommandFulfilled || result.CleanupFulfillment != CommandUnfulfilled || len(result.SystemPACCleanup.Issues) != 1 || len(result.CleanupFailures) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if pac.cleanupCalls != 1 || !pac.reachableDuringCleanup {
		t.Fatalf("cleanup calls/reachable = %d/%t", pac.cleanupCalls, pac.reachableDuringCleanup)
	}
	if conn, err := net.DialTimeout("tcp", pac.endpoint, 100*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("PAC endpoint remained reachable after Stop")
	}
}

func TestStopQuiescesAdmittedDeliveryAndRejectsLaterDelivery(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents(nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	pac := &quiescingPAC{deliverEntered: make(chan struct{}), releaseDeliver: make(chan struct{}), cleanupEntered: make(chan struct{})}
	lifecycle, err := newLifecycle(pac, emptyTestUserCA{}, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRuntime{engine: runtime, ctx: ctx, cancel: cancel, phase: runtimePhaseRunning}
	lifecycle.runtime = active
	deliveryDone := make(chan struct{})
	go func() { _, _ = lifecycle.deliverSystemPAC(ctx, active); close(deliveryDone) }()
	<-pac.deliverEntered
	stopDone := make(chan StopResult, 1)
	go func() { result, _ := lifecycle.Stop(context.Background()); stopDone <- result }()
	deadline := time.Now().Add(time.Second)
	for {
		lifecycle.mu.Lock()
		ending := lifecycle.ownerEnding
		lifecycle.mu.Unlock()
		if ending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Stop did not enter Owner Ending")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-pac.cleanupEntered:
		t.Fatal("cleanup began before admitted delivery settled")
	case <-time.After(100 * time.Millisecond):
	}
	close(pac.releaseDeliver)
	<-deliveryDone
	result := <-stopDone
	if result.CleanupFulfillment != CommandFulfilled {
		t.Fatalf("result = %#v", result)
	}
	if _, delivered := lifecycle.deliverSystemPAC(context.Background(), active); delivered || pac.deliverCalls != 1 {
		t.Fatalf("post-cleanup delivery admitted=%t calls=%d", delivered, pac.deliverCalls)
	}
}

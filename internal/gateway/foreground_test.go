package gateway

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSuperviseOwnerCancelsPendingActivationOnCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	activationReturned := make(chan struct{})
	done := make(chan ownerEvent, 1)
	go func() {
		done <- superviseOwner(
			ctx,
			func(ctx context.Context) error {
				<-ctx.Done()
				close(activationReturned)
				return ctx.Err()
			},
			make(chan struct{}),
			make(chan error),
			make(chan error),
		)
	}()

	cancel()
	event := receiveOwnerEvent(t, done)
	if event.kind != ownerEventContextDone {
		t.Fatalf("event kind = %d, want context done", event.kind)
	}
	select {
	case <-activationReturned:
	default:
		t.Fatal("supervisor returned before canceled activation exited")
	}
}

func TestSuperviseOwnerCancelsPendingActivationAndPropagatesFatalError(t *testing.T) {
	fatal := make(chan error, 1)
	done := make(chan ownerEvent, 1)
	want := errors.New("runtime failed")
	go func() {
		done <- superviseOwner(
			context.Background(),
			func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			make(chan struct{}),
			fatal,
			make(chan error),
		)
	}()

	fatal <- want
	event := receiveOwnerEvent(t, done)
	if event.kind != ownerEventFatalRuntime {
		t.Fatalf("event kind = %d, want fatal runtime", event.kind)
	}
	if !errors.Is(event.err, want) {
		t.Fatalf("event error = %v, want %v", event.err, want)
	}
}

func TestSuperviseOwnerContinuesAfterActivationCompletes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	activated := make(chan struct{})
	done := make(chan ownerEvent, 1)
	go func() {
		done <- superviseOwner(
			ctx,
			func(context.Context) error {
				close(activated)
				return nil
			},
			make(chan struct{}),
			make(chan error),
			make(chan error),
		)
	}()

	select {
	case <-activated:
	case <-time.After(time.Second):
		t.Fatal("activation did not complete")
	}
	select {
	case event := <-done:
		t.Fatalf("supervisor returned after successful activation: %#v", event)
	default:
	}
	cancel()
	event := receiveOwnerEvent(t, done)
	if event.kind != ownerEventContextDone {
		t.Fatalf("event kind = %d, want context done", event.kind)
	}
}

func TestUnexpectedRouterTerminationExecutesOwnerStop(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	lease, acquired, err := coord.AcquireOwnershipLease()
	if err != nil || !acquired {
		t.Fatalf("acquire ownership lease = %t, %v", acquired, err)
	}
	settings := &lifecycleTestSystemSettings{}
	owner, err := newOwnerWithCoordinator(settings, emptyTestUserCA{}, coord)
	if err != nil {
		t.Fatal(err)
	}
	owner.lease = lease
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- owner.Run(context.Background(), func(context.Context) error {
			close(ready)
			return nil
		})
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("owner did not publish")
	}
	if err := owner.router.server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("owner run error = %v", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("owner did not end after Router termination")
	}
	if settings.uninstallCalls != 1 {
		t.Fatalf("Managed PAC uninstall calls = %d, want 1", settings.uninstallCalls)
	}
	if coord.Exists() {
		t.Fatal("unexpected Router termination preserved Gateway State Cache")
	}
}

func receiveOwnerEvent(t *testing.T, events <-chan ownerEvent) ownerEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for owner event")
		return ownerEvent{}
	}
}

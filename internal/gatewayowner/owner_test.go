package gatewayowner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSuperviseOwnerCancelsPendingActivationOnLeaseLoss(t *testing.T) {
	leaseLost := make(chan struct{})
	activationReturned := make(chan struct{})
	done := make(chan ownerEvent, 1)
	go func() {
		done <- superviseOwner(
			context.Background(),
			func(ctx context.Context) error {
				<-ctx.Done()
				close(activationReturned)
				return ctx.Err()
			},
			leaseLost,
			make(chan struct{}),
			make(chan error),
			make(chan error),
		)
	}()

	close(leaseLost)
	event := receiveOwnerEvent(t, done)
	if event.kind != ownerEventLeaseLost {
		t.Fatalf("event kind = %d, want lease lost", event.kind)
	}
	select {
	case <-activationReturned:
	default:
		t.Fatal("supervisor returned before canceled activation exited")
	}
}

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

package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
)

type owner struct {
	coord     *coordinator
	cache     stateCache
	listener  net.Listener
	lifecycle *lifecycle
	router    *routerServer
	lock      *ownerLock
}

func newOwnerWithCoordinator(pac managedPACModule, ca userCAModule, coord *coordinator) (*owner, error) {
	return newOwner(pac, ca, coord, true)
}

func newTransientOwnerWithCoordinator(pac managedPACModule, ca userCAModule, coord *coordinator) (*owner, error) {
	return newOwner(pac, ca, coord, false)
}

func newOwner(pac managedPACModule, ca userCAModule, coord *coordinator, inspectUserCA bool) (*owner, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("router listener unavailable: %w", err)
	}
	routerListen := listener.Addr().String()
	var lifecycle *lifecycle
	if inspectUserCA {
		lifecycle, err = newLifecycle(pac, ca, coord, routerListen)
	} else {
		lifecycle, err = newLifecycleUninspected(pac, ca, coord, routerListen)
	}
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	cache := stateCache{HTTPRouterListen: routerListen, Token: token}
	lifecycle.SetOwnerCache(cache)
	router := newRouter(token, lifecycle)
	return &owner{
		coord:     coord,
		cache:     cache,
		listener:  listener,
		lifecycle: lifecycle,
		router:    router,
	}, nil
}

func (o *owner) Run(ctx context.Context, afterPublish func(context.Context) error) error {
	if o.lock == nil {
		return fmt.Errorf("Gateway Owner requires Gateway Ownership Lock")
	}
	defer func() {
		_ = o.lock.Release()
		o.lock = nil
	}()
	errs := make(chan error, 1)
	go func() { errs <- o.router.Serve(o.listener) }()
	if err := o.coord.Claim(o.cache); err != nil {
		_ = o.router.Close(context.Background())
		return err
	}
	defer o.coord.RemoveOwned(o.cache)
	event := superviseOwner(ctx, afterPublish, o.router.ShutdownRequested(), o.lifecycle.FatalRuntimeErrors(), errs)
	switch event.kind {
	case ownerEventContextDone:
		return o.stopAndClose(event.err)
	case ownerEventShutdownRequested:
		_ = o.router.Close(context.Background())
		return nil
	case ownerEventFatalRuntime:
		return o.stopAndClose(event.err)
	case ownerEventRouterStopped:
		// A Router that terminates without an explicit Stop still executes the
		// same terminal Owner Stop path before ownership is released.
		if event.err == http.ErrServerClosed {
			event.err = nil
		}
		return o.stopAndClose(event.err)
	case ownerEventActivationFailed:
		_ = o.closeOwnerOnly(context.Background())
		return event.err
	default:
		return fmt.Errorf("unknown gateway owner event %d", event.kind)
	}
}

type ownerStopCleanupError struct{ failures []CleanupFailure }

func (ownerStopCleanupError) Error() string { return "owner stop left gateway cleanup residue" }

func (o *owner) stopAndClose(cause error) error {
	result, stopErr := o.lifecycle.Stop(context.Background())
	closeErr := o.router.Close(context.Background())
	if result.Fulfillment() == CommandUnfulfilled {
		stopErr = errors.Join(stopErr, ownerStopCleanupError{failures: append([]CleanupFailure(nil), result.CleanupFailures...)})
	}
	return errors.Join(cause, stopErr, closeErr)
}

func (o *owner) Shutdown(ctx context.Context) error {
	_, _ = o.lifecycle.Stop(ctx)
	return o.closeOwnerOnly(ctx)
}

func (o *owner) closeOwnerOnly(ctx context.Context) error {
	_ = o.coord.RemoveOwned(o.cache)
	return o.router.Close(ctx)
}

type ownerEventKind int

const (
	ownerEventContextDone ownerEventKind = iota
	ownerEventShutdownRequested
	ownerEventFatalRuntime
	ownerEventRouterStopped
	ownerEventActivationFailed
)

type ownerEvent struct {
	kind ownerEventKind
	err  error
}

// superviseOwner keeps ownership events observable while activation is pending.
// Every interruption cancels the activation context and waits for the callback
// to acknowledge cancellation before its caller tears down owned resources.
func superviseOwner(
	ctx context.Context,
	afterPublish func(context.Context) error,
	shutdownRequested <-chan struct{},
	fatalRuntimeErrors <-chan error,
	routerErrors <-chan error,
) ownerEvent {
	activationCtx, cancelActivation := context.WithCancel(ctx)
	defer cancelActivation()

	var activationDone <-chan error
	if afterPublish != nil {
		done := make(chan error, 1)
		activationDone = done
		go func() {
			done <- afterPublish(activationCtx)
		}()
	}

	for {
		select {
		case err := <-activationDone:
			activationDone = nil
			if ctx.Err() != nil {
				return ownerEvent{kind: ownerEventContextDone}
			}
			if err != nil {
				return ownerEvent{kind: ownerEventActivationFailed, err: err}
			}
		case <-ctx.Done():
			cancelAndWait(cancelActivation, activationDone)
			return ownerEvent{kind: ownerEventContextDone}
		case <-shutdownRequested:
			cancelAndWait(cancelActivation, activationDone)
			return ownerEvent{kind: ownerEventShutdownRequested}
		case err := <-fatalRuntimeErrors:
			cancelAndWait(cancelActivation, activationDone)
			return ownerEvent{kind: ownerEventFatalRuntime, err: err}
		case err := <-routerErrors:
			cancelAndWait(cancelActivation, activationDone)
			return ownerEvent{kind: ownerEventRouterStopped, err: err}
		}
	}
}

func cancelAndWait(cancel context.CancelFunc, activationDone <-chan error) {
	cancel()
	if activationDone != nil {
		<-activationDone
	}
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

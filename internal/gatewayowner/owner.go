package gatewayowner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"

	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayfacade"
	"seamless-cors/internal/gatewayrouter"
	"seamless-cors/internal/platform"
)

type Owner struct {
	coord    *gatewaycoord.Coordinator
	cache    gatewaycoord.GatewayStateCache
	listener net.Listener
	facade   *gatewayfacade.Facade
	router   *gatewayrouter.Server
}

func New(adapter platform.Adapter) (*Owner, error) {
	coord, err := gatewaycoord.Default()
	if err != nil {
		return nil, err
	}
	return NewWithCoord(adapter, coord)
}

func NewWithCoord(adapter platform.Adapter, coord *gatewaycoord.Coordinator) (*Owner, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("router listener unavailable: %w", err)
	}
	routerListen := listener.Addr().String()
	facade, err := gatewayfacade.New(adapter, coord, routerListen)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	cache := gatewaycoord.GatewayStateCache{HTTPRouterListen: routerListen, Token: token}
	facade.SetOwnerCache(cache)
	router := gatewayrouter.New(token, facade)
	return &Owner{
		coord:    coord,
		cache:    cache,
		listener: listener,
		facade:   facade,
		router:   router,
	}, nil
}

func (o *Owner) Facade() *gatewayfacade.Facade {
	return o.facade
}

func (o *Owner) Run(ctx context.Context, afterPublish func(context.Context) error) error {
	errs := make(chan error, 1)
	go func() { errs <- o.router.Serve(o.listener) }()
	if err := o.coord.Claim(o.cache); err != nil {
		_ = o.router.Close(context.Background())
		return err
	}
	defer o.coord.RemoveOwned(o.cache)
	leaseLost := watchLease(ctx, o.coord, o.cache)
	event := superviseOwner(ctx, afterPublish, leaseLost, o.router.ShutdownRequested(), o.facade.FatalRuntimeErrors(), errs)
	switch event.kind {
	case ownerEventContextDone, ownerEventLeaseLost:
		_, _ = o.facade.Stop()
		_ = o.router.Close(context.Background())
		return nil
	case ownerEventShutdownRequested:
		_ = o.router.Close(context.Background())
		return nil
	case ownerEventFatalRuntime:
		_, _ = o.facade.Stop()
		_ = o.router.Close(context.Background())
		return event.err
	case ownerEventRouterStopped:
		if event.err == nil || event.err == http.ErrServerClosed {
			return nil
		}
		return event.err
	case ownerEventActivationFailed:
		_ = o.closeOwnerOnly(context.Background())
		return event.err
	default:
		return fmt.Errorf("unknown gateway owner event %d", event.kind)
	}
}

type ownerEventKind int

const (
	ownerEventContextDone ownerEventKind = iota
	ownerEventLeaseLost
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
	leaseLost <-chan struct{},
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
		case <-leaseLost:
			cancelAndWait(cancelActivation, activationDone)
			return ownerEvent{kind: ownerEventLeaseLost}
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

func (o *Owner) Shutdown(ctx context.Context) error {
	_, _ = o.facade.Stop()
	return o.closeOwnerOnly(ctx)
}

func (o *Owner) closeOwnerOnly(ctx context.Context) error {
	_ = o.coord.RemoveOwned(o.cache)
	return o.router.Close(ctx)
}

func watchLease(ctx context.Context, coord *gatewaycoord.Coordinator, cache gatewaycoord.GatewayStateCache) <-chan struct{} {
	lost := make(chan struct{})
	go func() {
		defer close(lost)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !coord.Owns(cache) {
					return
				}
			}
		}
	}()
	return lost
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

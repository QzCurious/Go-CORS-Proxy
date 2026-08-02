package gateway

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

type commandHandler interface {
	ExecuteStart(context.Context, StartRequest) (StartResult, error)
	Stop(context.Context) (StopResult, error)
	Status(context.Context, bool) (StatusResult, error)
	Install(context.Context) (InstallResult, error)
	UninstallWithConsent(context.Context, string) (UninstallResult, error)
}

type routerServer struct {
	server     *http.Server
	token      string
	handler    commandHandler
	shutdownCh chan struct{}
}

var configureGatewayErrorsOnce sync.Once

func newRouter(token string, handler commandHandler) *routerServer {
	s := &routerServer{
		token:      token,
		handler:    handler,
		shutdownCh: make(chan struct{}),
	}
	router := chi.NewMux()
	router.Get("/health", s.health)
	api := humachi.New(router, gatewayRouterConfig())
	s.register(api)
	s.server = &http.Server{Handler: router}
	return s
}

func (s *routerServer) Serve(listener net.Listener) error {
	return s.server.Serve(listener)
}

func (s *routerServer) Close(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *routerServer) ShutdownRequested() <-chan struct{} {
	return s.shutdownCh
}

func (s *routerServer) register(api huma.API) {
	huma.Register(api, s.commandOperation(api, http.MethodPost, "/start", "start", "Start"), s.start)
	huma.Register(api, s.commandOperation(api, http.MethodPost, "/stop", "stop", "Stop"), s.stop)
	huma.Register(api, s.commandOperation(api, http.MethodGet, "/status", "status", "Status"), s.status)
	huma.Register(api, s.commandOperation(api, http.MethodPost, "/install", "install", "Install UserCA"), s.install)
	huma.Register(api, s.commandOperation(api, http.MethodPost, "/uninstall", "uninstall", "Uninstall UserCA"), s.uninstall)
}

func (s *routerServer) commandOperation(api huma.API, method, path, operationID, summary string) huma.Operation {
	return huma.Operation{
		Method:      method,
		Path:        path,
		OperationID: operationID,
		Summary:     summary,
		Tags:        []string{"Gateway Router"},
		Errors: []int{
			http.StatusUnauthorized,
			http.StatusConflict,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
			http.StatusServiceUnavailable,
		},
		Security:    []map[string][]string{{"gatewayOwnerToken": []string{}}},
		Middlewares: huma.Middlewares{s.authorize(api)},
	}
}

func (s *routerServer) authorize(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if s.token != "" && ctx.Header(tokenHeader) == s.token {
			next(ctx)
			return
		}
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
	}
}

func (s *routerServer) health(w http.ResponseWriter, req *http.Request) {
	if s.token == "" || req.Header.Get(tokenHeader) != s.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type startInput struct {
	Body *StartRequest
}

type startOutput struct {
	Body startSuccessBody
}

func (s *routerServer) start(ctx context.Context, input *startInput) (*startOutput, error) {
	request := StartRequest{}
	if input.Body != nil {
		request = *input.Body
	}
	result, err := s.handler.ExecuteStart(ctx, request)
	if err != nil {
		return nil, newRouterError(http.StatusInternalServerError, "Gateway could not produce a Start result.", err)
	}
	if result == nil {
		return nil, newRouterError(http.StatusInternalServerError, "Gateway could not produce a Start result.")
	}
	if result.Fulfillment() == CommandUnfulfilled {
		failure := startFailureRepresentation(result.Kind())
		return nil, newGatewayError(failure.status, string(result.Kind()), failure.message, startFailureDetailsFrom(result))
	}
	return &startOutput{Body: startSuccessBodyFrom(result)}, nil
}

type stopOutput struct {
	Body stopSuccessBody
}

func (s *routerServer) stop(ctx context.Context, _ *struct{}) (*stopOutput, error) {
	result, err := s.handler.Stop(ctx)
	if err != nil {
		return nil, newRouterError(http.StatusInternalServerError, "Gateway could not produce a Stop result.", err)
	}
	if result.Fulfillment() == CommandUnfulfilled {
		return nil, newGatewayError(http.StatusInternalServerError, string(result.Kind), "Gateway cleanup did not complete.", stopFailureDetailsFrom(result))
	}
	if result.Kind == StopResultStopped {
		go func() {
			time.Sleep(25 * time.Millisecond)
			s.requestShutdown()
			_ = s.server.Close()
		}()
	}
	return &stopOutput{Body: stopSuccessBodyFrom(result)}, nil
}

type statusOutput struct {
	Body StatusReport
}

func (s *routerServer) status(ctx context.Context, _ *struct{}) (*statusOutput, error) {
	result, err := s.handler.Status(ctx, false)
	if err != nil {
		return nil, newRouterError(http.StatusInternalServerError, "Gateway could not produce a Status result.", err)
	}
	if result.Fulfillment() == CommandUnfulfilled {
		return nil, newGatewayError(http.StatusServiceUnavailable, string(result.Kind), "Gateway ownership is transitioning; retry Status.", nil)
	}
	return &statusOutput{Body: result.StatusReport}, nil
}

type installOutput struct {
	Body installSuccessBody
}

func (s *routerServer) install(ctx context.Context, _ *struct{}) (*installOutput, error) {
	result, err := s.handler.Install(ctx)
	if err != nil {
		return nil, newRouterError(http.StatusInternalServerError, "Gateway could not produce an Install result.", err)
	}
	if result.Fulfillment() == CommandUnfulfilled {
		failure := installFailureRepresentation(result.Kind)
		return nil, newGatewayError(failure.status, string(result.Kind), failure.message, installFailureDetailsFrom(result))
	}
	return &installOutput{Body: installSuccessBodyFrom(result)}, nil
}

type uninstallOutput struct {
	Body uninstallSuccessBody
}

type uninstallInput struct {
	Body UninstallRequest
}

func (s *routerServer) uninstall(ctx context.Context, input *uninstallInput) (*uninstallOutput, error) {
	result, err := s.handler.UninstallWithConsent(ctx, input.Body.ConsentFingerprint)
	if err != nil {
		return nil, newRouterError(http.StatusInternalServerError, "Gateway could not produce an Uninstall result.", err)
	}
	if result.Fulfillment() == CommandUnfulfilled {
		failure := uninstallFailureRepresentation(result.Kind)
		return nil, newGatewayError(failure.status, string(result.Kind), failure.message, uninstallFailureDetailsFrom(result))
	}
	return &uninstallOutput{Body: uninstallSuccessBodyFrom(result)}, nil
}

func (s *routerServer) requestShutdown() {
	select {
	case <-s.shutdownCh:
	default:
		close(s.shutdownCh)
	}
}

func gatewayRouterConfig() huma.Config {
	configureGatewayErrorsOnce.Do(func() {
		huma.NewError = func(status int, message string, errs ...error) huma.StatusError {
			return newRouterError(status, message, errs...)
		}
		huma.NewErrorWithContext = func(_ huma.Context, status int, message string, errs ...error) huma.StatusError {
			return newRouterError(status, message, errs...)
		}
	})
	config := huma.DefaultConfig("seamless-cors Gateway Router", "0.0.0")
	config.CreateHooks = nil
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"gatewayOwnerToken": {
			Type:        "apiKey",
			Name:        tokenHeader,
			In:          "header",
			Description: "Gateway Owner token from the local Gateway State Cache.",
		},
	}
	return config
}

type failureRepresentation struct {
	status  int
	message string
}

func startFailureRepresentation(kind StartKind) failureRepresentation {
	switch kind {
	case StartResultOwnerTransition:
		return failureRepresentation{http.StatusServiceUnavailable, "Gateway ownership is transitioning; retry Start."}
	case StartResultConsentRequired:
		return failureRepresentation{http.StatusUnprocessableEntity, "Managed PAC consent is required."}
	case StartResultConsentDeclined:
		return failureRepresentation{http.StatusUnprocessableEntity, "Managed PAC consent was declined."}
	case StartResultNoManageablePACServices:
		return failureRepresentation{http.StatusUnprocessableEntity, "No manageable PAC services are available."}
	case StartResultStartAlreadyMutating:
		return failureRepresentation{http.StatusConflict, "Another Gateway mutation is in progress."}
	case StartResultStopCancelled:
		return failureRepresentation{http.StatusConflict, "Stop cancelled the Start operation."}
	case StartResultCleanupFailed:
		return failureRepresentation{http.StatusInternalServerError, "Gateway cleanup did not complete."}
	default:
		return failureRepresentation{http.StatusInternalServerError, "Gateway Start was not fulfilled."}
	}
}

func installFailureRepresentation(kind InstallResultKind) failureRepresentation {
	switch kind {
	case InstallResultApprovalDenied:
		return failureRepresentation{http.StatusUnprocessableEntity, "Certificate trust approval was denied."}
	case InstallResultOwnerTransition:
		return failureRepresentation{http.StatusServiceUnavailable, "Gateway ownership is transitioning; retry Install."}
	case InstallResultAlreadyMutating:
		return failureRepresentation{http.StatusConflict, "Another certificate operation is in progress."}
	case InstallResultOwnerEnding:
		return failureRepresentation{http.StatusConflict, "The Gateway owner is ending."}
	case InstallResultRuntimeAdoptionFailed:
		return failureRepresentation{http.StatusInternalServerError, "The User CA was installed, but runtime adoption did not complete."}
	default:
		return failureRepresentation{http.StatusInternalServerError, "Gateway Install was not fulfilled."}
	}
}

func uninstallFailureRepresentation(kind UninstallResultKind) failureRepresentation {
	switch kind {
	case UninstallResultConsentRequired:
		return failureRepresentation{http.StatusUnprocessableEntity, "Confirmation is required while HTTPS interception is active."}
	case UninstallResultOwnerTransition:
		return failureRepresentation{http.StatusServiceUnavailable, "Gateway ownership is transitioning; retry Uninstall."}
	case UninstallResultAlreadyMutating:
		return failureRepresentation{http.StatusConflict, "Another certificate operation is in progress."}
	case UninstallResultOwnerEnding:
		return failureRepresentation{http.StatusConflict, "The Gateway owner is ending."}
	case UninstallResultIncomplete:
		return failureRepresentation{http.StatusInternalServerError, "User CA removal did not complete."}
	default:
		return failureRepresentation{http.StatusInternalServerError, "Gateway Uninstall was not fulfilled."}
	}
}

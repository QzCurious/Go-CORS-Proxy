package gatewayrouter

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"seamless-cors/internal/gatewayfacade"
)

const tokenHeader = "X-Seamless-CORS-Token"

type Facade interface {
	ExecuteStart(context.Context, gatewayfacade.StartRequest) (gatewayfacade.StartResult, error)
	Stop() (gatewayfacade.StopResult, error)
	Status(bool) (gatewayfacade.StatusResult, error)
	Install() (gatewayfacade.InstallResult, error)
	Uninstall() (gatewayfacade.UninstallResult, error)
}

type Server struct {
	server     *http.Server
	token      string
	facade     Facade
	shutdownCh chan struct{}
}

func New(token string, facade Facade) *Server {
	s := &Server{
		token:      token,
		facade:     facade,
		shutdownCh: make(chan struct{}),
	}
	router := chi.NewMux()
	router.Get("/health", s.health)
	api := humachi.New(router, gatewayRouterConfig())
	s.register(api)
	s.server = &http.Server{Handler: router}
	return s
}

func (s *Server) Serve(listener net.Listener) error {
	return s.server.Serve(listener)
}

func (s *Server) Close(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) ShutdownRequested() <-chan struct{} {
	return s.shutdownCh
}

func (s *Server) register(api huma.API) {
	huma.Register(api, s.commandOperation(api, http.MethodPost, "/start", "start", "Start"), s.start)
	huma.Register(api, s.commandOperation(api, http.MethodPost, "/stop", "stop", "Stop"), s.stop)
	huma.Register(api, s.commandOperation(api, http.MethodGet, "/status", "status", "Status"), s.status)
	huma.Register(api, s.commandOperation(api, http.MethodPost, "/install", "install", "Install UserCA"), s.install)
	huma.Register(api, s.commandOperation(api, http.MethodPost, "/uninstall", "uninstall", "Uninstall UserCA"), s.uninstall)
}

func (s *Server) commandOperation(api huma.API, method, path, operationID, summary string) huma.Operation {
	return huma.Operation{
		Method:      method,
		Path:        path,
		OperationID: operationID,
		Summary:     summary,
		Tags:        []string{"Gateway Router"},
		Errors:      []int{http.StatusUnauthorized},
		Security:    []map[string][]string{{"gatewayOwnerToken": []string{}}},
		Middlewares: huma.Middlewares{s.authorize(api)},
	}
}

func (s *Server) authorize(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if s.token != "" && ctx.Header(tokenHeader) == s.token {
			next(ctx)
			return
		}
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
	}
}

func (s *Server) health(w http.ResponseWriter, req *http.Request) {
	if s.token == "" || req.Header.Get(tokenHeader) != s.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) start(_ context.Context, input *startInput) (*startOutput, error) {
	request := gatewayfacade.StartRequest{}
	if input.Body != nil {
		request = *input.Body
	}
	result, err := s.facade.ExecuteStart(context.Background(), request)
	if err != nil {
		var startErr *gatewayfacade.StartError
		if errors.As(err, &startErr) {
			return nil, &startHTTPError{
				Status:   http.StatusInternalServerError,
				Title:    "gateway start failed",
				Detail:   startErr.Diagnostic,
				CAEnsure: startErr.CAEnsure,
			}
		}
		return nil, err
	}
	return &startOutput{Body: result}, nil
}

type startHTTPError struct {
	Status   int                           `json:"status"`
	Title    string                        `json:"title"`
	Detail   string                        `json:"detail"`
	CAEnsure *gatewayfacade.CAEnsureResult `json:"caEnsure,omitempty"`
}

func (e *startHTTPError) Error() string  { return e.Detail }
func (e *startHTTPError) GetStatus() int { return e.Status }

func (s *Server) stop(_ context.Context, _ *struct{}) (*stopOutput, error) {
	result, err := s.facade.Stop()
	if err != nil {
		return nil, err
	}
	if result.Kind == gatewayfacade.StopResultStopped {
		go func() {
			time.Sleep(25 * time.Millisecond)
			s.requestShutdown()
			_ = s.server.Close()
		}()
	}
	return &stopOutput{Body: result}, nil
}

func (s *Server) status(_ context.Context, _ *struct{}) (*statusOutput, error) {
	result, err := s.facade.Status(false)
	if err != nil {
		return nil, err
	}
	return &statusOutput{Body: result}, nil
}

func (s *Server) install(_ context.Context, _ *struct{}) (*installOutput, error) {
	result, err := s.facade.Install()
	if err != nil {
		return nil, err
	}
	return &installOutput{Body: result}, nil
}

func (s *Server) uninstall(_ context.Context, _ *struct{}) (*uninstallOutput, error) {
	result, err := s.facade.Uninstall()
	if err != nil {
		return nil, err
	}
	return &uninstallOutput{Body: result}, nil
}

func (s *Server) requestShutdown() {
	select {
	case <-s.shutdownCh:
	default:
		close(s.shutdownCh)
	}
}

func gatewayRouterConfig() huma.Config {
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

type startInput struct {
	Body *gatewayfacade.StartRequest
}

type startOutput struct {
	Body gatewayfacade.StartResult
}

type stopOutput struct {
	Body gatewayfacade.StopResult
}

type statusOutput struct {
	Body gatewayfacade.StatusResult
}

type installOutput struct {
	Body gatewayfacade.InstallResult
}

type uninstallOutput struct {
	Body gatewayfacade.UninstallResult
}

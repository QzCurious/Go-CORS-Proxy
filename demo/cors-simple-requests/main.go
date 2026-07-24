// Command cors-simple-requests demonstrates a simple cross-origin request to
// a deliberately CORS-unaware API.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	webListen = "127.0.0.1:4000"
	apiListen = "127.0.0.1:4100"
	apiOrigin = "http://api.127.0.0.1.nip.io:4100"
)

//go:embed index.html
var demoHTML string

func main() {
	logger := log.New(os.Stdout, "", log.Ltime)
	webServer := &http.Server{
		Addr:              webListen,
		Handler:           webHandler(apiOrigin),
		ReadHeaderTimeout: 5 * time.Second,
	}
	apiServer := &http.Server{
		Addr:              apiListen,
		Handler:           apiHandler(logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 2)
	go serve(logger, "API", apiServer, errs)
	go serve(logger, "Web app", webServer, errs)

	fmt.Println()
	fmt.Println("CORS simple request demonstration")
	fmt.Println("Web app:           http://127.0.0.1:4000")
	fmt.Println("API request:       " + apiOrigin + "/api/message")
	fmt.Println("Domain List entry: " + apiOrigin)
	fmt.Println()
	fmt.Println("The API intentionally sends no Access-Control-Allow-* headers.")
	fmt.Println("Press Ctrl+C to stop both demo servers.")
	fmt.Println()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
	case err := <-errs:
		logger.Printf("demo stopped: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = webServer.Shutdown(shutdownCtx)
	_ = apiServer.Shutdown(shutdownCtx)
}

func serve(logger *log.Logger, name string, server *http.Server, errs chan<- error) {
	logger.Printf("%s listening on http://%s", name, server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s: %w", name, err)
	}
}

func apiHandler(logger *log.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/message", func(w http.ResponseWriter, r *http.Request) {
		logger.Printf("API received GET /api/message from Origin %q", r.Header.Get("Origin"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Demo-Server", "cors-unaware-api")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "Hello from the CORS-unaware API!",
			"time":    time.Now().Format(time.RFC3339),
		})
	})
	return mux
}

func webHandler(origin string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, strings.ReplaceAll(demoHTML, "{{API_URL}}", origin+"/api/message"))
	})
	return mux
}

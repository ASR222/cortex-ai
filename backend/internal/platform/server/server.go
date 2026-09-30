// Package server runs an HTTP server with structured logging and graceful
// shutdown, shared by every Go service's main function.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// SetupLogger installs a JSON slog logger tagged with the service name.
func SetupLogger(service string) {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h).With("service", service))
}

// Run serves handler on addr until SIGINT/SIGTERM, then drains in-flight
// requests for up to 20 seconds. onShutdown runs after the server stops (for
// closing database connections).
func Run(addr string, handler http.Handler, onShutdown func(context.Context)) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: agent responses stream for up to a few minutes.
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	case sig := <-stop:
		slog.Info("shutting down", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
	if onShutdown != nil {
		onShutdown(ctx)
	}
}

// HandleHealthcheckCommand implements `<binary> healthcheck`, used by Docker
// health checks. The runtime images are distroless (no shell or curl), so the
// service binary checks its own /healthz endpoint.
func HandleHealthcheckCommand(defaultAddr string) {
	if len(os.Args) < 2 || os.Args[1] != "healthcheck" {
		return
	}
	addr := defaultAddr
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + addr + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	os.Exit(0)
}

// Fatal logs err and exits. Used for startup failures.
func Fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}

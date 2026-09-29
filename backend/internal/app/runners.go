package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/touchline/backend/internal/scheduler"
)

// RunAPI serves the HTTP + WebSocket surface until ctx is cancelled, draining
// in-flight requests on shutdown.
func (a *App) RunAPI(ctx context.Context) error {
	srv := &http.Server{
		Addr:              ":" + a.APIPort,
		Handler:           a.HTTPHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("API server starting on :%s (cors origin %s)", a.APIPort, a.AppOrigin)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	return nil
}

// RunScheduler drives the configurable world clock until ctx is cancelled. A
// scheduler advisory lock elects a single leader.
func (a *App) RunScheduler(ctx context.Context) error {
	log.Printf("world clock starting (cadence sync every %s)", a.Poll)
	if err := scheduler.NewService(a.Pool, a.Bus).Run(ctx, a.Poll); err != nil {
		return fmt.Errorf("world clock: %w", err)
	}
	log.Printf("world clock stopped")
	return nil
}

// RunAll runs the API, scheduler, and worker in one process until ctx is
// cancelled or one of them fails. All three share the same pool, bus, and
// realtime transport built by Build.
func (a *App) RunAll(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Printf("---- touchline serve: api :%s + scheduler + worker in one process ----", a.APIPort)

	errCh := make(chan error, 3)
	go func() { errCh <- a.RunAPI(ctx) }()
	go func() { errCh <- a.RunScheduler(ctx) }()
	go func() { errCh <- a.RunWorker(ctx) }()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("touchline serve: subsystem failed (%v); shutting down", err)
		}
		cancel()
		// Give the other subsystems a moment to drain after cancellation.
		timeout := time.NewTimer(20 * time.Second)
		defer timeout.Stop()
		for range 2 {
			select {
			case <-errCh:
			case <-timeout.C:
			}
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ServeHealth exposes the liveness endpoint used by Docker Compose for the
// standalone scheduler and worker binaries. It blocks until the listener fails.
func ServeHealth(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	log.Printf("health server starting on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Printf("health server: %v", err)
	}
}

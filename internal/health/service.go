package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rafaeljc/heimdall/internal/config"
)

// Service manages the dedicated HTTP server for health checks and metrics.
// It isolates observability traffic from the main application traffic.
type Service struct {
	server *http.Server
	checks []Checker
	cfg    config.HealthConfig
	logger *slog.Logger
}

// NewService creates a configured health service instance.
// It requires a configured logger, the application config, and a variadic list of checkers.
func NewService(logger *slog.Logger, cfg *config.Config, checks ...Checker) *Service {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.NoCache)

	svc := &Service{
		checks: checks,
		cfg:    cfg.Health,
		logger: logger,
		server: &http.Server{
			Addr:         ":" + cfg.Health.Port,
			Handler:      r,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			// mitigation against Slowloris attacks
			ReadHeaderTimeout: 2 * time.Second,
		},
	}

	// Register routes using configured paths
	r.Get(svc.cfg.LivenessPath, svc.liveness)
	r.Get(svc.cfg.ReadinessPath, svc.readiness)

	return svc
}

// Start runs the health server in a background goroutine.
// This method is non-blocking.
func (s *Service) Start() {
	go func() {
		s.logger.Info("starting health check server",
			slog.String("port", s.cfg.Port),
			slog.String("liveness_path", s.cfg.LivenessPath),
			slog.String("readiness_path", s.cfg.ReadinessPath),
		)

		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("failed to start health server", slog.String("error", err.Error()))
		}
	}()
}

// Stop performs a graceful shutdown of the health server.
func (s *Service) Stop(ctx context.Context) error {
	s.logger.Info("stopping health server")
	return s.server.Shutdown(ctx)
}

// --- Handlers ---

// liveness returns 200 OK if the HTTP server is running.
// It is used by Kubernetes to restart the pod if the process is deadlocked.
func (s *Service) liveness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// readiness checks all registered dependencies.
// It returns 200 OK only if all checkers pass. Used by Kubernetes to route traffic.
func (s *Service) readiness(w http.ResponseWriter, r *http.Request) {
	// Use the configured timeout to ensure we reply to Kubernetes in time.
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()

	statusMap := make(map[string]string)
	hasError := false

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, checker := range s.checks {
		wg.Add(1)
		go func(c Checker) {
			defer wg.Done()

			// Execute the check with the context timeout
			err := c.Check(ctx)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				// Log as WARN to avoid alerting noise, as Kubernetes will retry.
				s.logger.Warn("health probe failed",
					slog.String("component", c.Name()),
					slog.String("error", err.Error()),
				)
				statusMap[c.Name()] = fmt.Sprintf("down: %v", err)
				hasError = true
			} else {
				statusMap[c.Name()] = "up"
			}
		}(checker)
	}

	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	if hasError {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	// We ignore the encoder error here as the status code is already written.
	// The JSON body is for human debugging, Kubernetes only cares about the status code.
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": statusMap,
	})
}

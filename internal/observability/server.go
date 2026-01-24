package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rafaeljc/heimdall/internal/config"
)

// Server manages the observability endpoints (health checks and metrics).
// It runs on a dedicated port to isolate administrative traffic from business traffic.
type Server struct {
	logger   *slog.Logger
	cfg      *config.ObservabilityConfig
	router   *chi.Mux
	server   *http.Server
	checkers []Checker
}

// NewServer creates a new instance of the observability server.
// It accepts a variable number of checkers (e.g., Postgres, Redis) to be verified in the readiness probe.
func NewServer(logger *slog.Logger, cfg *config.ObservabilityConfig, checkers ...Checker) *Server {
	r := chi.NewRouter()

	// Standard middlewares for the admin server
	r.Use(middleware.Recoverer)
	r.Use(middleware.NoCache)

	s := &Server{
		logger:   logger,
		cfg:      cfg,
		router:   r,
		checkers: checkers,
	}

	s.setupRoutes()

	return s
}

// setupRoutes registers all observability endpoints.
func (s *Server) setupRoutes() {
	// Liveness Probe: Used by K8s to know if the pod is running.
	s.router.Get(s.cfg.LivenessPath, s.liveness)

	// Readiness Probe: Used by K8s to know if the pod is ready to accept traffic.
	s.router.Get(s.cfg.ReadinessPath, s.readiness)

	// Metrics Endpoint: Used by Prometheus to scrape telemetry data.
	// promhttp.Handler() automatically exposes all metrics defined in metrics.go.
	s.router.Method(http.MethodGet, s.cfg.MetricsPath, promhttp.Handler())
}

// Start runs the HTTP server in a background goroutine.
// It is non-blocking.
func (s *Server) Start() {
	addr := fmt.Sprintf(":%s", s.cfg.Port)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  s.cfg.Timeout,
		WriteTimeout: s.cfg.Timeout,
		IdleTimeout:  s.cfg.Timeout * 3,
	}

	go func() {
		s.logger.Info("starting observability server",
			slog.String("addr", addr),
			slog.String("liveness_path", s.cfg.LivenessPath),
			slog.String("readiness_path", s.cfg.ReadinessPath),
			slog.String("metrics_path", s.cfg.MetricsPath),
		)

		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("observability server failed", slog.String("error", err.Error()))
		}
	}()
}

// Shutdown gracefully stops the observability server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("stopping observability server")
	return s.server.Shutdown(ctx)
}

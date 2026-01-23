package observability

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rafaeljc/heimdall/internal/config"
)

// Server manages the observability endpoints (health checks and metrics).
type Server struct {
	logger   *slog.Logger
	cfg      *config.Config
	router   *chi.Mux
	server   *http.Server
	checkers []Checker
}

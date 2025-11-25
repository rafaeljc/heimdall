// Package controlapi implements the REST API for the Heimdall Control Plane.
package controlapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"

	"github.com/rafaeljc/heimdall/internal/store"
)

// API is the main struct that holds dependencies and the router for the Control Plane.
// It follows the Dependency Injection pattern to facilitate testing.
type API struct {
	// Router is the Chi multiplexer that handles HTTP requests.
	Router *chi.Mux

	// flags is the data access layer for feature flags.
	// We use the interface type to allow for mocking in unit tests.
	flags store.FlagRepository
}

// NewAPI creates a new API instance and configures its routes.
func NewAPI(flagRepo store.FlagRepository) *API {
	// We check the interface explicitly.
	// An interface is only nil if it has no underlying type and no value.
	if flagRepo == nil {
		panic("controlapi: flag repository cannot be nil")
	}

	api := &API{
		Router: chi.NewRouter(),
		flags:  flagRepo,
	}

	api.configureRoutes()
	return api
}

// configureRoutes registers the global middleware stack and API endpoints.
func (a *API) configureRoutes() {
	// 1. Global Middleware Stack
	// RequestID: Adds a unique ID to each request context (essential for tracing).
	a.Router.Use(middleware.RequestID)
	// RealIP: correctly sets the IP if behind a proxy/LB.
	a.Router.Use(middleware.RealIP)
	// Logger: Logs request method, path, status, and duration.
	a.Router.Use(middleware.Logger)
	// Recoverer: Prevents the server from crashing on panics, returning 500 instead.
	a.Router.Use(middleware.Recoverer)
	// Content-Type: Forces JSON content type for API responses.
	a.Router.Use(render.SetContentType(render.ContentTypeJSON))

	// 2. System Routes
	a.Router.Get("/health", a.handleHealthCheck)

	// 3. API V1 Routes
	a.Router.Route("/api/v1", func(r chi.Router) {
		r.Route("/flags", func(r chi.Router) {
			r.Post("/", a.handleCreateFlag)
			r.Get("/", a.handleListFlags)

			r.Route("/{key}", func(r chi.Router) {
				r.Get("/", a.handleGetFlag)
				r.Patch("/", a.handleUpdateFlag)
				r.Delete("/", a.handleDeleteFlag)
			})
		})
	})
}

// handleHealthCheck verifies if the service is healthy and can connect to the database.
// Note: To implement a Readiness Probe (Deep Check), we should extend the
// FlagRepository interface to include a Ping() method. For now, this checks HTTP serving capability.
func (a *API) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	render.Status(r, http.StatusOK)
	render.JSON(w, r, map[string]string{"status": "ok"})
}

// --- Handler Stubs (To be implemented) ---

func (a *API) handleGetFlag(w http.ResponseWriter, r *http.Request) {
	render.Status(r, http.StatusNotImplemented)
	render.JSON(w, r, ErrorResponse{Code: "NOT_IMPLEMENTED", Message: "Endpoint coming soon"})
}

func (a *API) handleUpdateFlag(w http.ResponseWriter, r *http.Request) {
	render.Status(r, http.StatusNotImplemented)
	render.JSON(w, r, ErrorResponse{Code: "NOT_IMPLEMENTED", Message: "Endpoint coming soon"})
}

func (a *API) handleDeleteFlag(w http.ResponseWriter, r *http.Request) {
	render.Status(r, http.StatusNotImplemented)
	render.JSON(w, r, ErrorResponse{Code: "NOT_IMPLEMENTED", Message: "Endpoint coming soon"})
}

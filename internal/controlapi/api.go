// Package controlapi implements the REST API for the Heimdall Control Plane.
package controlapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafaeljc/heimdall/internal/validation"
)

// API is the main struct that holds dependencies and the router for the Control Plane.
// It follows the Dependency Injection pattern to facilitate testing.
type API struct {
	// Router is the Chi multiplexer that handles HTTP requests.
	Router *chi.Mux

	// db is the connection pool to PostgreSQL.
	// In the future, this should be an interface (Repository Pattern) for better mocking.
	db *pgxpool.Pool
}

// NewAPI creates a new API instance and configures its routes.
// It requires a valid database connection pool to be injected.
func NewAPI(db *pgxpool.Pool) *API {
	validation.AssertNotNil(db, "database pool")

	api := &API{
		Router: chi.NewRouter(),
		db:     db,
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
// It returns 200 OK if healthy, or 503 Service Unavailable if the DB is unreachable.
func (a *API) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	// Ping the database to ensure connectivity
	if err := a.db.Ping(r.Context()); err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, map[string]string{
			"status": "unhealthy",
			"error":  "Database unreachable",
		})
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, map[string]string{"status": "ok"})
}

// --- Handler Stubs (To be implemented) ---

func (a *API) handleCreateFlag(w http.ResponseWriter, r *http.Request) {
	render.Status(r, http.StatusNotImplemented)
	render.JSON(w, r, ErrorResponse{Code: "NOT_IMPLEMENTED", Message: "Endpoint coming soon"})
}

func (a *API) handleListFlags(w http.ResponseWriter, r *http.Request) {
	render.Status(r, http.StatusNotImplemented)
	render.JSON(w, r, ErrorResponse{Code: "NOT_IMPLEMENTED", Message: "Endpoint coming soon"})
}

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

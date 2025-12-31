// Package controlapi implements the REST API for the Heimdall Control Plane.
package controlapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"

	"github.com/rafaeljc/heimdall/internal/cache"
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

	// cache is used to publish flag update events to subscribers.
	cache cache.Service

	// apiKeyHash is the SHA-256 hash of the valid API key.
	// Used for authentication in production environments.
	apiKeyHash string

	// skipAuth disables authentication when true (test/dev environments only).
	// Production environments should always set this to false.
	skipAuth bool
}

// NewAPI creates a new API instance with authentication enabled by default.
// The apiKeyHash parameter must be the SHA-256 hash of the API key.
// Panics if apiKeyHash is empty, as authentication cannot be disabled with this constructor.
func NewAPI(flagRepo store.FlagRepository, cacheSvc cache.Service, apiKeyHash string) *API {
	return NewAPIWithConfig(flagRepo, cacheSvc, apiKeyHash, false)
}

// NewAPIWithConfig creates a new API instance with explicit control over authentication.
// This constructor is primarily used in tests to disable authentication.
//
// Parameters:
//   - flagRepo: Repository for flag operations
//   - cacheSvc: Cache service for event publishing
//   - apiKeyHash: SHA-256 hash of the valid API key (required if skipAuth is false)
//   - skipAuth: If true, disables authentication (USE ONLY IN TESTS)
//
// Panics if:
//   - flagRepo or cacheSvc are nil
//   - apiKeyHash is empty when skipAuth is false
func NewAPIWithConfig(flagRepo store.FlagRepository, cacheSvc cache.Service, apiKeyHash string, skipAuth bool) *API {
	// We check the interface explicitly.
	// An interface is only nil if it has no underlying type and no value.
	if flagRepo == nil {
		panic("controlapi: flag repository cannot be nil")
	}
	if cacheSvc == nil {
		panic("controlapi: cache service cannot be nil")
	}

	// Validate authentication configuration
	if !skipAuth && apiKeyHash == "" {
		panic("controlapi: apiKeyHash cannot be empty when authentication is enabled")
	}

	api := &API{
		Router:     chi.NewRouter(),
		flags:      flagRepo,
		cache:      cacheSvc,
		apiKeyHash: apiKeyHash,
		skipAuth:   skipAuth,
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
	a.Router.Use(RequestLogger)
	// Recoverer: Prevents the server from crashing on panics, returning 500 instead.
	a.Router.Use(middleware.Recoverer)
	// Content-Type: Forces JSON content type for API responses.
	a.Router.Use(render.SetContentType(render.ContentTypeJSON))

	// 2. Public Routes (no authentication required)
	a.Router.Get("/health", a.handleHealthCheck)

	// 3. Protected API V1 Routes (authentication required)
	a.Router.Route("/api/v1", func(r chi.Router) {
		// Apply authentication middleware to all /api/v1/* routes
		r.Use(a.authenticateAPIKey)

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

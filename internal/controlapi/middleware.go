package controlapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLogger creates a middleware that logs the start and end of each request.
// It integrates with slog to provide structured logs including RequestID, Method, Path, Status, and Duration.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Get RequestID set by Chi's RequestID middleware
		reqID := middleware.GetReqID(r.Context())

		// Wrap the ResponseWriter to capture the status code
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		// Process the request
		next.ServeHTTP(ww, r)

		// Calculate duration
		duration := time.Since(start)

		// Log the completed request
		// We use Info level for success, Warn for 4xx, Error for 5xx
		level := slog.LevelInfo
		status := ww.Status()

		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}

		slog.Log(r.Context(), level, "HTTP request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration", duration.String(),
			"request_id", reqID,
			"remote_ip", r.RemoteAddr,
		)
	})
}

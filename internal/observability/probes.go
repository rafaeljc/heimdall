package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
)

// liveness responds with 200 OK if the HTTP server is running.
// It is used by Kubernetes to restart the pod if the process is deadlocked.
func (s *Server) liveness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// readiness checks all registered dependencies.
// Returns 200 OK only if all checkers pass. Used by Kubernetes to route traffic.
func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	// Enforce the configured timeout to ensure we respond to Kubernetes in time.
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()

	statusMap := make(map[string]string)
	hasError := false

	var wg sync.WaitGroup
	var mu sync.Mutex

	// Execute checks in parallel
	for _, checker := range s.checkers {
		wg.Add(1)
		go func(c Checker) {
			defer wg.Done()

			// Run the check respecting the context timeout
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

	// We ignore the encoder error because the status code has already been written.
	// The JSON body is for human debugging; Kubernetes only cares about the status code.
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": statusMap,
	})
}

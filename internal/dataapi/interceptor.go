package dataapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// RequestLoggerInterceptor returns a UnaryServerInterceptor that handles structured logging.
// It performs three critical tasks for observability:
// 1. Traceability: Extracts or generates a Request ID.
// 2. Context Injection: Injects a logger into the context for the handler to use.
// 3. Telemetry: Logs the duration and status of the RPC call.
func RequestLoggerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		// 1. Resolve Request ID
		// In gRPC, headers are passed via Metadata.
		// We look for "x-request-id" (standard convention).
		reqID := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			// metadata map keys are normalized to lowercase
			if ids := md.Get("x-request-id"); len(ids) > 0 {
				reqID = ids[0]
			}
		}

		// If missing, we generate one to ensure traceability is never broken.
		if reqID == "" {
			reqID = uuid.NewString()
		}

		// 2. Create Contextual Logger
		// We create a derived logger. This is cheap (shallow copy of the handler).
		rpcLogger := slog.Default().With(
			slog.String("request_id", reqID),
			slog.String("rpc_method", info.FullMethod),
		)

		// 3. Inject Logger into Context
		// The handler (e.g., ResolveFlag) can now call logger.FromContext(ctx).
		newCtx := logger.WithContext(ctx, rpcLogger)

		// 4. Handle the RPC
		resp, err := handler(newCtx, req)

		// 5. Log Outcome
		duration := time.Since(start)
		st, _ := status.FromError(err) // Safe extraction of gRPC status
		code := st.Code()

		// Determine Log Level based on gRPC Code
		// OK/Canceled/NotFound -> Info (Expected behavior)
		// Internal/Unavailable -> Error (System failure)
		level := slog.LevelInfo
		switch code {
		case codes.Internal, codes.Unavailable, codes.DataLoss, codes.Unknown:
			level = slog.LevelError
		case codes.DeadlineExceeded, codes.Unimplemented:
			level = slog.LevelWarn
		}

		// Perform the Log
		// We use Log() to pass the dynamic level.
		// Optimized: Attributes are typed to minimize allocation.
		rpcLogger.Log(newCtx, level, "grpc request completed",
			slog.String("code", code.String()),
			slog.Duration("duration", duration),
			slog.String("peer_addr", getPeerAddr(ctx)), // Helper optional
		)

		return resp, err
	}
}

// ObservabilityInterceptor returns a UnaryServerInterceptor that collects RED metrics.
// It acts as the telemetry middleware, measuring:
// 1. Request Duration (Latency) -> heimdall_data_plane_grpc_handling_seconds
// 2. Request Count (Traffic/Errors) -> heimdall_data_plane_grpc_requests_total
//
// SAFETY NOTE: This implementation uses 'GetMetricWithLabelValues' instead of 'WithLabelValues'.
// The standard 'WithLabelValues' panics if the number of labels mismatches the definition.
// In a critical Data Plane, we prefer to log a telemetry error rather than crashing the request handling.
func ObservabilityInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		// Execute the handler
		resp, err := handler(ctx, req)

		// Metrics Calculation
		duration := time.Since(start).Seconds()

		// Extract gRPC Status Code safely
		st, _ := status.FromError(err)
		codeStr := st.Code().String()
		method := info.FullMethod

		// We retrieve the logger from context to ensure any telemetry errors
		// are correlated with the Request ID.
		log := logger.FromContext(ctx)

		// ---------------------------------------------------------------------
		// 1. Record Latency (Histogram)
		// ---------------------------------------------------------------------
		histObserver, metricErr := observability.DataPlaneGrpcDuration.GetMetricWithLabelValues(method, codeStr)
		if metricErr != nil {
			// Log telemetry error but DO NOT fail the request
			log.Warn("observability: failed to record latency metric",
				slog.String("error", metricErr.Error()),
				slog.String("metric", "grpc_handling_seconds"),
			)
		} else {
			histObserver.Observe(duration)
		}

		// ---------------------------------------------------------------------
		// 2. Record Count (Counter)
		// ---------------------------------------------------------------------
		counter, metricErr := observability.DataPlaneGrpcTotal.GetMetricWithLabelValues(method, codeStr)
		if metricErr != nil {
			log.Warn("observability: failed to record request count metric",
				slog.String("error", metricErr.Error()),
				slog.String("metric", "grpc_requests_total"),
			)
		} else {
			counter.Inc()
		}

		return resp, err
	}
}

// getPeerAddr is a helper to extract client IP safely
func getPeerAddr(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok {
		return p.Addr.String()
	}
	return "unknown"
}

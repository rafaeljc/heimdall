package dataapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"strings"
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

// AuthInterceptor returns a UnaryServerInterceptor that enforces API key authentication.
//
// Authentication Mechanism:
// The interceptor validates incoming requests using Bearer token authentication.
// The client must provide an Authorization header in the format:
//
//	Authorization: Bearer <api-key>
//
// The provided api-key is hashed using SHA-256 and compared against expectedHash
// using constant-time comparison to prevent timing attacks.
//
// Parameters:
//
//	expectedHash: The SHA-256 hash of the valid API key in hexadecimal format.
//	             This should be pre-computed server-side and stored securely.
//	             Example: sha256("my-secret-key") = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
//
// Security Features:
//   - Constant-time comparison (subtle.ConstantTimeCompare) prevents timing attacks
//   - Bearer token validation ensures standard format compliance
//   - Detailed internal logging for debugging while returning generic error messages
//     to clients to avoid information disclosure
//
// Middleware Integration:
// This interceptor should be registered in the gRPC server options AFTER RequestLoggerInterceptor
// so that authentication failure logs are properly correlated with the request ID:
//
//	grpc.ChainUnaryInterceptor(
//	    dataapi.RequestLoggerInterceptor(),    // Outer: Injects RequestID and Logger
//	    dataapi.AuthInterceptor(expectedHash), // Middle: Validates authentication
//	    dataapi.ObservabilityInterceptor(),    // Inner: Collects metrics
//	)
//
// Error Responses:
//   - Unauthenticated (missing metadata): No Authorization header was sent
//   - Unauthenticated (invalid format): Authorization header doesn't follow "Bearer <token>" format
//   - Unauthenticated (empty token): Bearer token is empty or whitespace
//   - Unauthenticated (invalid key): The provided key doesn't match the expected hash
func AuthInterceptor(expectedHash string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// Step 1: Extract metadata from incoming gRPC request
		// In gRPC, HTTP headers are exposed as metadata.
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		// Step 2: Retrieve the Authorization header
		// Metadata keys are normalized to lowercase by gRPC.
		authHeader := md.Get("authorization")
		if len(authHeader) == 0 || authHeader[0] == "" {
			return nil, status.Error(codes.Unauthenticated, "authorization header is required")
		}

		// Step 3: Validate Bearer token format
		// We expect: "Bearer <api-key>"
		tokenStr := authHeader[0]
		if !strings.HasPrefix(tokenStr, "Bearer ") {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization format: expected Bearer token")
		}

		// Step 4: Extract and validate the token itself
		clientKey := strings.TrimPrefix(tokenStr, "Bearer ")
		if clientKey == "" {
			return nil, status.Error(codes.Unauthenticated, "bearer token is empty")
		}

		// Step 5: Hash the provided key and compare
		// We use SHA-256 to hash the client-provided key, then compare it against
		// the expectedHash using constant-time comparison.
		// This prevents attackers from using timing differences to infer the correct key.
		hash := sha256.Sum256([]byte(clientKey))
		clientHashHex := hex.EncodeToString(hash[:])

		// subtle.ConstantTimeCompare returns 1 if equal, 0 if different.
		if subtle.ConstantTimeCompare([]byte(clientHashHex), []byte(expectedHash)) != 1 {
			log := logger.FromContext(ctx)
			log.Warn("authentication failed: invalid api key in bearer token",
				slog.String("method", info.FullMethod),
			)

			return nil, status.Error(codes.Unauthenticated, "invalid api key")
		}

		// Step 6: Authentication successful, proceed to handler
		return handler(ctx, req)
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

//go:build integration

package dataapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/rafaeljc/heimdall/internal/dataapi"
	pb "github.com/rafaeljc/heimdall/proto/heimdall/v1"
)

// noOpService is a minimal gRPC service for testing interceptors.
type noOpService struct {
	pb.UnimplementedDataPlaneServiceServer
}

// setupAuthInterceptorTestEnv creates a single gRPC server with the AuthInterceptor.
// Shared across all tests to avoid redundant setup.
func setupAuthInterceptorTestEnv(apiKeyHash string) (pb.DataPlaneServiceClient, func()) {
	// Setup gRPC server with ONLY AuthInterceptor
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer(
		grpc.UnaryInterceptor(dataapi.AuthInterceptor(apiKeyHash)),
	)
	pb.RegisterDataPlaneServiceServer(s, &noOpService{})

	// Start server
	go func() {
		if err := s.Serve(lis); err != nil {
			panic(fmt.Sprintf("Server exited with error: %v", err))
		}
	}()

	// Create client
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(err)
	}

	client := pb.NewDataPlaneServiceClient(conn)

	// Cleanup function
	cleanup := func() {
		_ = conn.Close()
		s.Stop()
	}

	return client, cleanup
}

// TestAuthInterceptor_Integration uses table-driven tests to validate Bearer token authentication.
func TestAuthInterceptor_Integration(t *testing.T) {
	// Generate test key and its SHA-256 hash
	testKey := "valid-test-key"
	hashObj := sha256.Sum256([]byte(testKey))
	testHash := hex.EncodeToString(hashObj[:])

	// Setup server once for all tests
	client, cleanup := setupAuthInterceptorTestEnv(testHash)
	defer cleanup()

	tests := []struct {
		name           string
		authHeader     string
		sdkHeader      string
		expectedCode   codes.Code
		expectedErrMsg string
	}{
		{
			name:           "Missing authorization header",
			authHeader:     "",
			sdkHeader:      "",
			expectedCode:   codes.Unauthenticated,
			expectedErrMsg: "authorization header is required",
		},
		{
			name:           "Invalid Bearer format",
			authHeader:     "InvalidFormat",
			sdkHeader:      "",
			expectedCode:   codes.Unauthenticated,
			expectedErrMsg: "expected Bearer token",
		},
		{
			name:           "Empty Bearer token",
			authHeader:     "Bearer ",
			sdkHeader:      "",
			expectedCode:   codes.Unauthenticated,
			expectedErrMsg: "bearer token is empty",
		},
		{
			name:           "Invalid API key",
			authHeader:     "Bearer wrong-key",
			sdkHeader:      "",
			expectedCode:   codes.Unauthenticated,
			expectedErrMsg: "invalid api key",
		},
		{
			name:           "Valid API key",
			authHeader:     "Bearer " + testKey,
			sdkHeader:      "",
			expectedCode:   codes.Unimplemented, // noOpService returns Unimplemented, not auth error
			expectedErrMsg: "",
		},
		{
			name:           "Valid API key with SDK header",
			authHeader:     "Bearer " + testKey,
			sdkHeader:      "node-sdk/1.2.3",
			expectedCode:   codes.Unimplemented,
			expectedErrMsg: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Build context with headers
			if tt.authHeader != "" {
				ctx = metadata.AppendToOutgoingContext(ctx, "authorization", tt.authHeader)
			}
			if tt.sdkHeader != "" {
				ctx = metadata.AppendToOutgoingContext(ctx, "x-heimdall-sdk", tt.sdkHeader)
			}

			// Make request
			_, err := client.Evaluate(ctx, &pb.EvaluateRequest{FlagKey: "test"})

			// Verify response
			st, ok := status.FromError(err)
			require.True(t, ok, "error should be a gRPC status error")
			assert.Equal(t, tt.expectedCode, st.Code(), "unexpected gRPC code")

			if tt.expectedErrMsg != "" {
				assert.Contains(t, st.Message(), tt.expectedErrMsg, "unexpected error message")
			}
		})
	}
}

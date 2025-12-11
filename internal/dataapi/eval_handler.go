// Package dataapi implements the gRPC Data Plane for flag evaluation.
//
// This file specifically implements the Evaluate RPC handler, responsible for
// orchestrating validation, cache lookup, and decision logic.
package dataapi

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/rafaeljc/heimdall/internal/logger"
	pb "github.com/rafaeljc/heimdall/proto/heimdall/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Evaluate evaluates a feature flag against the provided user context.
//
// It retrieves the flag configuration from the L2 Cache (Redis) and determines
// the resulting boolean value. Currently, it supports static on/off toggles based
// on the "enabled" field.
//
// It returns:
//   - OK with the boolean value if successful.
//   - NOT_FOUND if the flag key does not exist.
//   - INVALID_ARGUMENT if the flag key is missing.
//   - INTERNAL if there is a connectivity issue with the cache.
func (a *API) Evaluate(ctx context.Context, req *pb.EvaluateRequest) (*pb.EvaluateResponse, error) {
	log := logger.FromContext(ctx)

	// 1. Input Validation (Fail Fast)
	if req.FlagKey == "" {
		// Log as Warn because it's a client error, not a server failure.
		log.Warn("bad request: missing flag_key")
		return nil, status.Error(codes.InvalidArgument, "flag_key is required")
	}

	// Trace the evaluation attempt (Debug level for high-throughput)
	log.Debug("evaluating flag", slog.String("flag_key", req.FlagKey))

	// 2. Retrieve from L2 Cache (Redis)
	// In the future (V2), this step will be preceded by an L1 memory cache lookup.
	rawMap, err := a.cache.GetFlag(ctx, req.FlagKey)
	if err != nil {
		// Log the internal error so SREs can debug connectivity issues.
		// We use structured logging to capture the error details clearly.
		log.Error("failed to fetch flag from cache",
			slog.String("flag_key", req.FlagKey),
			slog.String("error", err.Error()),
		)
		return nil, status.Errorf(codes.Internal, "failed to fetch flag configuration")
	}

	// Redis HGETALL returns an empty map (not nil) if the key does not exist.
	if len(rawMap) == 0 {
		// Debug log is enough here; NotFound is a valid business state.
		log.Debug("flag not found in cache", slog.String("flag_key", req.FlagKey))
		return nil, status.Errorf(codes.NotFound, "flag %q not found", req.FlagKey)
	}

	// 3. Evaluation Logic (V1 - Static Toggle)
	// Redis stores booleans as strings. We use ParseBool to handle "1", "t", "true", etc.
	// If parsing fails (or field is missing), it defaults to false (Safe Fallback).
	enabledStr := rawMap["enabled"]
	enabled, _ := strconv.ParseBool(enabledStr)

	// 4. Construct Response
	return &pb.EvaluateResponse{
		Value:  enabled,
		Reason: "STATIC_V1", // Debug info indicating no complex rules were evaluated
	}, nil
}

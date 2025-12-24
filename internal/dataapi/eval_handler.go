// Package dataapi implements the gRPC Data Plane for flag evaluation.
//
// This file specifically implements the Evaluate RPC handler, responsible for
// orchestrating validation, cache lookup, and decision logic.
package dataapi

import (
	"context"
	"log/slog"

	"github.com/rafaeljc/heimdall/internal/logger"
	"github.com/rafaeljc/heimdall/internal/ruleengine"
	pb "github.com/rafaeljc/heimdall/proto/heimdall/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Evaluate evaluates a feature flag against the provided user context using a
// Read-Through caching strategy.
//
// Flow: L1 (Memory) -> L2 (Redis) -> Engine -> Response
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

	// 2. L1 Cache Check (In-Memory, Lock-Free)
	flag, found := a.l1.Get(req.FlagKey)

	if !found {
		// 3. L2 Cache Check (Redis)
		var err error
		flag, err = a.l2.GetFlag(ctx, req.FlagKey)
		if err != nil {
			log.Error("failed to fetch flag from l2", slog.String("flag_key", req.FlagKey), slog.String("error", err.Error()))
			// We return Internal here to be safe and avoid exposing internal errors to clients.
			return nil, status.Error(codes.Internal, "failed to retrieve flag configuration")
		}

		if flag == nil {
			return nil, status.Error(codes.NotFound, "flag not found")
		}

		// 4. Cache Fill (Read-Through)
		a.l1.Set(req.FlagKey, flag)
	}

	// 5. Optimization: Short-Circuit
	// If the flag is globally disabled, we can short-circuit the evaluation.
	if !flag.Enabled {
		return &pb.EvaluateResponse{
			Value:  false,
			Reason: "DISABLED",
		}, nil
	}

	// 6. Context Preparation
	attributes := make(map[string]string, len(req.Context))
	var userID string

	for k, v := range req.Context {
		if k == "user_id" {
			userID = v
		}
		attributes[k] = v // Strings are immutable, cheap to copy
	}

	input := ruleengine.EvaluationInput{
		FlagKey: req.FlagKey,
		User: ruleengine.Context{
			UserID:     userID,
			Attributes: attributes,
		},
	}

	// 7. Rule Evaluation
	match := a.engine.Evaluate(flag.Rules, input)

	// 8. Determine final value and reason
	var value bool
	var reason string

	if match {
		value = true
		reason = "RULE_MATCH"
	} else {
		// No rules matched - use the default value
		value = flag.DefaultValue
		reason = "NO_MATCH"
	}

	return &pb.EvaluateResponse{
		Value:  value,
		Reason: reason,
	}, nil
}

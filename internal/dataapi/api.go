// Package dataapi implements the gRPC Data Plane for flag evaluation.
// It handles the high-performance read path for client SDKs.
package dataapi

import (
	"github.com/rafaeljc/heimdall/internal/cache"
	pb "github.com/rafaeljc/heimdall/proto/heimdall/v1"
	"google.golang.org/grpc"
)

// API implements the gRPC DataPlane service defined in the proto contract.
// It embeds the UnimplementedDataPlaneServer for forward compatibility.
type API struct {
	pb.UnimplementedDataPlaneServer
	cache cache.Service
}

// NewAPI creates a new Data Plane gRPC API instance.
func NewAPI(cacheSvc cache.Service) *API {
	if cacheSvc == nil {
		panic("dataapi: cache service cannot be nil")
	}

	return &API{
		cache: cacheSvc,
	}
}

// Register connects this implementation to the grpc.Server engine.
// This encapsulates the registration logic, keeping main.go clean.
func (a *API) Register(grpcServer *grpc.Server) {
	pb.RegisterDataPlaneServer(grpcServer, a)
}

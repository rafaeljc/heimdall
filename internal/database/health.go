package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthChecker implements the observability.Checker interface for PostgreSQL.
type HealthChecker struct {
	pool *pgxpool.Pool
}

// NewHealthChecker creates a new health checker for the given database connection.
func NewHealthChecker(pool *pgxpool.Pool) *HealthChecker {
	return &HealthChecker{pool: pool}
}

// Name returns the component name.
func (h *HealthChecker) Name() string {
	return "postgres"
}

// Check verifies the database connection using PingContext.
func (h *HealthChecker) Check(ctx context.Context) error {
	if h.pool == nil {
		return fmt.Errorf("database connection is nil")
	}
	return h.pool.Ping(ctx)
}

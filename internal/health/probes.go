package health

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// --- Postgres Probe ---

type postgresChecker struct {
	pool *pgxpool.Pool
}

// NewPostgresChecker creates a health checker for a pgx connection pool.
func NewPostgresChecker(pool *pgxpool.Pool) Checker {
	return &postgresChecker{pool: pool}
}

func (p *postgresChecker) Name() string {
	return "database"
}

func (p *postgresChecker) Check(ctx context.Context) error {
	// Enforce a strict timeout for the ping to prevent blocking the health check loop
	// if the context passed by the caller is too loose.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return p.pool.Ping(ctx)
}

// --- Redis Probe ---

type redisChecker struct {
	client *redis.Client
}

// NewRedisChecker creates a health checker for a go-redis client.
func NewRedisChecker(client *redis.Client) Checker {
	return &redisChecker{client: client}
}

func (r *redisChecker) Name() string {
	return "redis"
}

func (r *redisChecker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	// go-redis Ping returns a *StatusCmd, we need to extract the error.
	return r.client.Ping(ctx).Err()
}

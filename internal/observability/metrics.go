package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// NOTE: Currently, all metrics are defined globally here.
// This causes a harmless side-effect where a service (e.g., data-plane)
// initializes metrics from other services (e.g., control-plane) with zero values.
//
// TODO(refactor): When the number of metrics grows significantly, split this
// package into sub-packages (metrics/data, metrics/control) to isolate initialization.

// namespace defines the global prefix for all metrics (e.g., heimdall_...).
const namespace = "heimdall"

// lowLatencyBuckets defines custom buckets for high-performance operations (Data Plane).
// Standard buckets are too coarse (starting at 5ms), so we add 1ms and 2ms resolution.
// Range: 1ms to 500ms.
var lowLatencyBuckets = []float64{.001, .002, .005, .010, .015, .020, .025, .030, .050, .100, .500}

var (
	// -------------------------------------------------------------------------
	// CONTROL PLANE (HTTP)
	// -------------------------------------------------------------------------

	// ControlPlaneReqDuration measures the latency of HTTP requests.
	// Metric: heimdall_control_plane_http_handling_seconds
	ControlPlaneReqDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "control_plane",
		Name:      "http_handling_seconds",
		Help:      "Time taken to handle HTTP requests in Control Plane",
		Buckets:   prometheus.DefBuckets, // Standard buckets are fine for Admin APIs (human speed)
	}, []string{"method", "route"})

	// ControlPlaneReqTotal counts the total number of HTTP requests.
	// Metric: heimdall_control_plane_http_requests_total
	ControlPlaneReqTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "control_plane",
		Name:      "http_requests_total",
		Help:      "Total HTTP requests in Control Plane",
	}, []string{"method", "route", "code"})

	// -------------------------------------------------------------------------
	// DATA PLANE (gRPC + Cache)
	// -------------------------------------------------------------------------

	// DataPlaneGrpcDuration measures the latency of gRPC evaluate requests.
	// Metric: heimdall_data_plane_grpc_handling_seconds
	DataPlaneGrpcDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "data_plane",
		Name:      "grpc_handling_seconds",
		Help:      "Time taken to handle gRPC evaluate requests",
		Buckets:   lowLatencyBuckets, // Custom buckets for < 20ms SLO
	}, []string{"method", "code"})

	// DataPlaneGrpcTotal counts the total number of gRPC requests.
	// Metric: heimdall_data_plane_grpc_requests_total
	DataPlaneGrpcTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "data_plane",
		Name:      "grpc_requests_total",
		Help:      "Total gRPC evaluate requests",
	}, []string{"method", "code"})

	// --- Cache L1 Metrics (Otter) ---

	DataPlaneCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "data_plane",
		Name:      "l1_cache_hits_total",
		Help:      "Total L1 cache hits (in-memory)",
	})

	DataPlaneCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "data_plane",
		Name:      "l1_cache_misses_total",
		Help:      "Total L1 cache misses",
	})

	// DataPlaneCacheEvictions tracks items removed due to memory pressure.
	// Essential for tuning MaxCost.
	DataPlaneCacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "data_plane",
		Name:      "l1_cache_evictions_total",
		Help:      "Total items evicted due to memory pressure/MaxCost",
	})

	// Note: Changed from 'usage_bytes' to 'items_count'
	// to reflect the capabilities of the S3-FIFO algorithm (Otter)
	// which tracks item count efficiently, but not byte size.
	DataPlaneCacheUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "data_plane",
		Name:      "l1_cache_items_count",
		Help:      "Current number of items in the L1 cache",
	})

	// DataPlaneCacheDropped tracks writes dropped because the buffer was full.
	// Indicates if the write throughput is too high for the cache configuration.
	DataPlaneCacheDropped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "data_plane",
		Name:      "l1_cache_dropped_total",
		Help:      "Total sets dropped due to write buffer contention",
	})

	DataPlaneInvalidations = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "data_plane",
		Name:      "l1_invalidations_total",
		Help:      "Total cache invalidation events received via PubSub",
	})

	// -------------------------------------------------------------------------
	// SYNCER (Workers)
	// -------------------------------------------------------------------------

	// SyncerJobDuration measures "Freshness" (Latency from Enqueue to Processed).
	// Metric: heimdall_syncer_job_processing_duration_seconds
	SyncerJobDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "syncer",
		Name:      "job_processing_duration_seconds",
		Help:      "End-to-end latency from enqueue to processing finish",
		Buckets:   prometheus.DefBuckets,
	})

	SyncerJobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "syncer",
		Name:      "jobs_total",
		Help:      "Total propagation jobs processed",
	}, []string{"status"}) // success, dlq, failure

	RedisQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "redis_queue_depth",
		Help:      "Current number of items in the update queue",
	})

	// -------------------------------------------------------------------------
	// INFRASTRUCTURE (DB & Cache Pools)
	// -------------------------------------------------------------------------

	// --- Database (Postgres) ---

	// DBPoolConnections tracks the state of connections in the PG pool.
	// Labels: state (idle, in_use, total, max)
	DBPoolConnections = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "database",
		Name:      "pool_connections",
		Help:      "Number of connections in the pool by state",
	}, []string{"state"})

	// DBPoolAcquireCount tracks how many times a connection was requested.
	DBPoolAcquireCount = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "database",
		Name:      "pool_acquire_count_total",
		Help:      "Total number of connection acquisition attempts",
	})

	// DBPoolAcquireDuration tracks the total time spent waiting for a connection.
	// Useful for calculating average wait time = duration / count.
	DBPoolAcquireDuration = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "database",
		Name:      "pool_acquire_duration_seconds_total",
		Help:      "Total time spent waiting for a connection",
	})

	// DBPoolWaitCount tracks how many times the pool was empty and caller had to wait.
	DBPoolWaitCount = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "database",
		Name:      "pool_wait_count_total",
		Help:      "Total number of times a connection request had to wait",
	})

	// --- Cache (Redis) ---

	// RedisPoolConnections tracks the state of connections in the Redis pool.
	// Labels: state (idle, total, stale)
	RedisPoolConnections = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "redis",
		Name:      "pool_connections",
		Help:      "Number of connections in the pool by state",
	}, []string{"state"})

	// RedisPoolHits tracks valid connections reused from the pool.
	RedisPoolHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "redis",
		Name:      "pool_hits_total",
		Help:      "Total number of times a connection was found in the pool",
	})

	// RedisPoolMisses tracks new connections created because pool was empty.
	RedisPoolMisses = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "redis",
		Name:      "pool_misses_total",
		Help:      "Total number of times a new connection had to be created",
	})

	// RedisPoolTimeouts tracks failures to obtain a connection.
	RedisPoolTimeouts = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "redis",
		Name:      "pool_timeouts_total",
		Help:      "Total number of connection pool timeouts",
	})
)

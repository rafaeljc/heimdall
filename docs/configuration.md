# Configuration Reference

All environment variables use the `HEIMDALL_` prefix.

## Quick Start

**Development:**
```bash
export HEIMDALL_DB_HOST=localhost
export HEIMDALL_DB_PORT=5432
export HEIMDALL_DB_NAME=heimdall
export HEIMDALL_DB_USER=heimdall
export HEIMDALL_DB_PASSWORD=dev_password
export HEIMDALL_REDIS_HOST=localhost
export HEIMDALL_REDIS_PORT=6379
export HEIMDALL_REDIS_PASSWORD=dev_redis_pass
```

**Production:**
```bash
export HEIMDALL_APP_ENV=production
export HEIMDALL_DB_HOST=db.example.com
export HEIMDALL_DB_PORT=5432
export HEIMDALL_DB_NAME=heimdall
export HEIMDALL_DB_USER=heimdall
export HEIMDALL_DB_PASSWORD=SuperSecure123!
export HEIMDALL_DB_SSL_MODE=require
export HEIMDALL_REDIS_HOST=redis.example.com
export HEIMDALL_REDIS_PORT=6379
export HEIMDALL_REDIS_PASSWORD=RedisSecure123!
export HEIMDALL_REDIS_TLS_ENABLED=true
export HEIMDALL_SERVER_CONTROL_API_KEY_HASH=<sha256-hash>
export HEIMDALL_SERVER_CONTROL_TLS_ENABLED=true
export HEIMDALL_SERVER_CONTROL_TLS_CERT_FILE=/certs/tls.crt
export HEIMDALL_SERVER_CONTROL_TLS_KEY_FILE=/certs/tls.key
```

## Environment Variables

### Application

| Variable | Default | Values |
|----------|---------|--------|
| `HEIMDALL_APP_NAME` | `heimdall` | - |
| `HEIMDALL_APP_VERSION` | `dev` | - |
| `HEIMDALL_APP_ENV` | `development` | `development`, `staging`, `production` |
| `HEIMDALL_APP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `HEIMDALL_APP_LOG_FORMAT` | `text` | `json`, `text` |
| `HEIMDALL_APP_SHUTDOWN_TIMEOUT` | `30s` | - |

### Control Plane (REST API)

| Variable | Default | Notes |
|----------|---------|-------|
| `HEIMDALL_SERVER_CONTROL_PORT` | `8080` | - |
| `HEIMDALL_SERVER_CONTROL_HOST` | `0.0.0.0` | - |
| `HEIMDALL_SERVER_CONTROL_READ_TIMEOUT` | `10s` | - |
| `HEIMDALL_SERVER_CONTROL_WRITE_TIMEOUT` | `10s` | - |
| `HEIMDALL_SERVER_CONTROL_READ_HEADER_TIMEOUT` | `5s` | - |
| `HEIMDALL_SERVER_CONTROL_IDLE_TIMEOUT` | `60s` | - |
| `HEIMDALL_SERVER_CONTROL_MAX_HEADER_BYTES` | `524288` | - |
| `HEIMDALL_SERVER_CONTROL_API_KEY_HASH` | - | SHA-256 hash, required in production |
| `HEIMDALL_SERVER_CONTROL_TLS_ENABLED` | `false` | Required in production |
| `HEIMDALL_SERVER_CONTROL_TLS_CERT_FILE` | - | Required if TLS enabled |
| `HEIMDALL_SERVER_CONTROL_TLS_KEY_FILE` | - | Required if TLS enabled |

### Data Plane (gRPC API)

| Variable | Default | Notes |
|----------|---------|-------|
| `HEIMDALL_SERVER_DATA_PORT` | `50051` | - |
| `HEIMDALL_SERVER_DATA_HOST` | `0.0.0.0` | - |
| `HEIMDALL_SERVER_DATA_MAX_CONCURRENT_STREAMS` | `100` | - |
| `HEIMDALL_SERVER_DATA_KEEPALIVE_TIME` | `120s` | - |
| `HEIMDALL_SERVER_DATA_KEEPALIVE_TIMEOUT` | `20s` | - |
| `HEIMDALL_SERVER_DATA_MAX_CONNECTION_AGE` | `300s` | - |
| `HEIMDALL_SERVER_DATA_L1_CACHE_CAPACITY` | `10000` | In-memory cache size, ≥1000 in production |
| `HEIMDALL_SERVER_DATA_L1_CACHE_TTL` | `60s` | In-memory cache TTL, ≥10s in production |

### Database

| Variable | Default | Notes |
|----------|---------|-------|
| `HEIMDALL_DB_URL` | - | Alternative to individual components |
| `HEIMDALL_DB_HOST` | - | Required if not using URL |
| `HEIMDALL_DB_PORT` | - | Required if not using URL |
| `HEIMDALL_DB_NAME` | - | Required if not using URL |
| `HEIMDALL_DB_USER` | - | Required if not using URL |
| `HEIMDALL_DB_PASSWORD` | - | Required in production |
| `HEIMDALL_DB_SSL_MODE` | `prefer` | `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full` |
| `HEIMDALL_DB_MAX_CONNS` | `25` | - |
| `HEIMDALL_DB_MIN_CONNS` | `2` | - |
| `HEIMDALL_DB_MAX_CONN_LIFETIME` | `1h` | - |
| `HEIMDALL_DB_MAX_CONN_IDLE_TIME` | `30m` | - |
| `HEIMDALL_DB_CONNECT_TIMEOUT` | `5s` | - |
| `HEIMDALL_DB_PING_MAX_RETRIES` | `5` | Number of ping attempts before failing |
| `HEIMDALL_DB_PING_BACKOFF` | `2s` | Initial backoff duration between ping retries (doubles each retry) |

### Redis

| Variable | Default | Notes |
|----------|---------|-------|
| `HEIMDALL_REDIS_URL` | - | Alternative to individual components |
| `HEIMDALL_REDIS_HOST` | - | Required if not using URL |
| `HEIMDALL_REDIS_PORT` | - | Required if not using URL |
| `HEIMDALL_REDIS_PASSWORD` | - | Required in production |
| `HEIMDALL_REDIS_DB` | `0` | 0-15 |
| `HEIMDALL_REDIS_TLS_ENABLED` | `false` | Required in production |
| `HEIMDALL_REDIS_POOL_SIZE` | `50` | - |
| `HEIMDALL_REDIS_MIN_IDLE_CONNS` | `10` | - |
| `HEIMDALL_REDIS_DIAL_TIMEOUT` | `5s` | - |
| `HEIMDALL_REDIS_READ_TIMEOUT` | `10s` | - |
| `HEIMDALL_REDIS_WRITE_TIMEOUT` | `3s` | - |
| `HEIMDALL_REDIS_POOL_TIMEOUT` | `4s` | - |
| `HEIMDALL_REDIS_MAX_RETRIES` | `3` | - |
| `HEIMDALL_REDIS_MIN_RETRY_BACKOFF` | `8ms` | - |
| `HEIMDALL_REDIS_MAX_RETRY_BACKOFF` | `512ms` | - |
| `HEIMDALL_REDIS_PING_MAX_RETRIES` | `5` | Number of ping attempts before failing |
| `HEIMDALL_REDIS_PING_BACKOFF` | `2s` | Initial backoff duration between ping retries (doubles each retry) |

### Syncer

| Variable | Default |
|----------|---------|
| `HEIMDALL_SYNCER_ENABLED` | `true` |
| `HEIMDALL_SYNCER_POP_TIMEOUT` | `5s` |
| `HEIMDALL_SYNCER_HYDRATION_CHECK_INTERVAL` | `10s` |
| `HEIMDALL_SYNCER_MAX_RETRIES` | `3` |
| `HEIMDALL_SYNCER_BASE_RETRY_DELAY` | `1s` |
| `HEIMDALL_SYNCER_HYDRATION_CONCURRENCY` | `10` |

### Observability

| Variable | Default |
|----------|---------|
| `HEIMDALL_OBSERVABILITY_PORT` | `9090` |
| `HEIMDALL_OBSERVABILITY_TIMEOUT` | `10s` |
| `HEIMDALL_OBSERVABILITY_LIVENESS_PATH` | `/healthz` |
| `HEIMDALL_OBSERVABILITY_READINESS_PATH` | `/readyz` |
| `HEIMDALL_OBSERVABILITY_METRICS_PATH` | `/metrics` |
| `HEIMDALL_OBSERVABILITY_METRICS_POLLING_INTERVAL` | `10s` |

## Production Requirements

When `HEIMDALL_APP_ENV=production`:
- Database password: ≥12 characters
- Database SSL mode: `require`, `verify-ca`, or `verify-full`
- Redis password: ≥12 characters
- Redis TLS: enabled
- Control plane API key hash: required
- Control plane TLS: enabled
- Data plane L1 cache capacity: ≥1000
- Data plane L1 cache TTL: ≥10s

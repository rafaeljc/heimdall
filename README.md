# Heimdall

Feature flag platform in Go. The admin API and the evaluation path run as
separate services, so operating one doesn't slow down the other.

[![CI Backend](https://github.com/rafaeljc/heimdall/actions/workflows/ci-backend.yml/badge.svg)](https://github.com/rafaeljc/heimdall/actions)
[![CI Infra](https://github.com/rafaeljc/heimdall/actions/workflows/ci-infra.yml/badge.svg)](https://github.com/rafaeljc/heimdall/actions)
[![CI Node SDK](https://github.com/rafaeljc/heimdall/actions/workflows/ci-node-sdk.yml/badge.svg)](https://github.com/rafaeljc/heimdall/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## Why it's built this way

Feature flag traffic is asymmetric. Services read flags on every request;
engineers change them a few times a day. Serving both from one database means
the read path inherits the write path's latency, and when that database slows
down, every service waiting on a flag slows down with it.

Heimdall splits the two. Writes go to Postgres and get ACID guarantees. Reads
never touch the database at all.

## Architecture

- **Control Plane (REST)** — the admin API. Operators create, update and delete
  flags here. Every mutation is persisted to PostgreSQL, which is the source of
  truth and enforces ACID guarantees.
- **Data Plane (gRPC)** — the evaluation path. Flags are served from a
  thread-safe in-process L1 cache backed by Redis as L2. The relational database
  is never queried. Backups, migrations and schema changes on the Control Plane
  cannot affect evaluation latency, because they are not on this path.
- **Syncer** — the bridge between the two. It subscribes to Control Plane
  mutations, transforms them into evaluation rules, publishes them to Redis and
  invalidates L1 across Data Plane instances. Consistency between the planes is
  eventual.
- **Client SDKs** — thin libraries that cache evaluations locally with a
  configurable TTL and refresh from the Data Plane once it expires. They own the
  gRPC connection lifecycle and degrade gracefully when the Data Plane is
  unreachable.

## Load test

Two runs with k6, in this order: measure one unit
first, then find out whether adding units helps.

**One pod, one vCPU.** Sustained over 10,000 requests per second with P99.9
latency under 20 ms.

![Heimdall Load Test - 10k RPS](https://github.com/user-attachments/assets/381b89e3-285d-4b60-9345-b8567724eb37)

The pod is capped at a single core (`1000m`) with the Go runtime pinned to match
(`GOMAXPROCS=1`). Running on one core removes cross-core lock contention and the
cache-line bouncing that comes with it, and in this configuration that is where
the throughput came from.

**Three pods.** Load and replicas raised together. Throughput grew close to
linearly. Three pods is where I stopped, not a ceiling I found.

**[Full Grafana snapshot of the load test](https://snapshots.raintank.io/dashboard/snapshot/By0pG1AWnXhbbuHjq5Y1t6mpvrMY03o7)**

These numbers come from synthetic traffic — a load generator, not real users.

## Quick start

**Prerequisites:** [Docker](https://docs.docker.com/get-docker/),
[Go 1.24+](https://golang.org/doc/install),
[Task](https://taskfile.dev/installation/),
[buf](https://buf.build/docs/installation),
[golangci-lint](https://github.com/golangci/golangci-lint).

```bash
# 1. Clone
git clone https://github.com/rafaeljc/heimdall.git && cd heimdall

# 2. Configure
cp .env.example .env

# 3. Generate an API key and its hash (or `task sec:hash` to hash an existing one)
task sec:genkey

# 4. Start
task dev:up
```

Read [docs/configuration.md](docs/configuration.md) before editing `.env` values.

| Service | Address |
| --- | --- |
| Control Plane (REST) | `http://localhost:8080` |
| API docs (Swagger UI) | `http://localhost:8081` |
| Data Plane (gRPC) | `localhost:50051` |
| Health checks | `:9090/healthz` · `:9091/healthz` · `:9092/healthz` |

`task dev:down` stops everything, `task --list-all` shows the rest.

## Infrastructure

- **Terraform** defines the AWS environment — VPC, EKS, Aurora and ElastiCache —
  in layers.
- **CI** validates infrastructure and application code separately. Infrastructure
  runs Terraform formatting with TFLint, security scanning with Checkov, and
  manifest validation with Kubeconform. Application runs linting and the test
  suites. Every check has to pass before deployment.
- **Kubernetes manifests** are Kustomize overlays under `infra/gitops/`, laid out
  for a pull-based ArgoCD sync, with database migrations as a PreSync hook.

## License

MIT — see [LICENSE](LICENSE).

# Heimdall: Enterprise-Grade Feature Flag Management

[![CI Backend](https://github.com/rafaeljc/heimdall/actions/workflows/ci-backend.yml/badge.svg)](https://github.com/rafaeljc/heimdall/actions)
[![CI Infra](https://github.com/rafaeljc/heimdall/actions/workflows/ci-infra.yml/badge.svg)](https://github.com/rafaeljc/heimdall/actions)
[![CI Node SDK](https://github.com/rafaeljc/heimdall/actions/workflows/ci-node-sdk.yml/badge.svg)](https://github.com/rafaeljc/heimdall/actions)

Heimdall is a distributed Feature Flag Management platform engineered for massive horizontal scalability and sub-millisecond evaluation latency at the edge.

## The Motivation (Why Heimdall?)

In large-scale microservice environments, feature flag evaluation is fundamentally asymmetric: thousands of services query flags on *every request* (constant reads), while engineers update flags *minutes or hours apart* (rare writes). Traditional single-database architectures crumble under this read-heavy load and the database becomes a latency bottleneck and a cascading failure point. When the database slows or goes offline, every service stalls, unable to evaluate flags.

**The Problem:** A single database cannot efficiently serve both high-throughput reads and strict consistency requirements.

**The Solution:** Heimdall solves this using a **CQRS-inspired architecture**. It strictly isolates the administrative mutation plane (ACID-compliant) from the high-throughput evaluation plane (in-memory caching), ensuring that backend administrative tasks never impact the latency of client evaluations.

## Architecture

Heimdall's design centers on the **CQRS pattern**: separating the slow, consistent write path from the fast, parallel read path.

* **Control Plane (REST):** The administrative interface for flag management. Operators create, update, and delete flags through this API. All mutations are persisted to **PostgreSQL**, which serves as the authoritative source of truth and enforces ACID guarantees. Changes are atomic and durable.

* **Data Plane (gRPC):** The high-performance evaluation engine serving client flag lookups. It uses a two-tier caching strategy: an in-process **L1 cache** (thread-safe, zero-latency) backed by **Redis** (L2, distributed cache), providing sub-millisecond evaluation latency. The relational database is never queried. This separation means operational tasks on the Control Plane (backups, migrations, schema changes) never impact the latency of client evaluations.

* **Syncer:** The bridge between the two planes. This asynchronous worker subscribes to Control Plane mutations, transforms them into optimized evaluation rules, and publishes them to Redis. It invalidates L1 caches across Data Plane instances, ensuring eventual consistency.

* **Client SDKs:** Lightweight, resilient libraries embedded in your applications. They cache flag evaluations locally with a configurable TTL, delivering microsecond-latency responses for cached entries and automatically refreshing from the Data Plane after TTL expiration. SDKs handle gRPC connection lifecycle, graceful degradation on network failures, and abstract away the complexity of the Data Plane protocol.

## Performance & Benchmarks

Heimdall is load-tested to validate reliability and performance under high traffic. In this test configuration, the system showed strong horizontal scaling behavior.

In this benchmark configuration, **a single Kubernetes Pod limited to 1 CPU (`1000m`) sustained over 10,000 Requests Per Second (RPS)** while maintaining **P99.9 latency under 20ms**.

![Heimdall Load Test - 10k RPS](https://github.com/user-attachments/assets/381b89e3-285d-4b60-9345-b8567724eb37)

**Single-core configuration note:** By pinning the Go runtime (`GOMAXPROCS=1`) and limiting the Pod to a single core, Heimdall reduces multi-core lock contention and CPU cache bouncing (L1 invalidations). In this configuration, that contributed to high throughput with low infrastructure overhead.

**[View the full interactive Grafana Snapshot of the Load Test](https://snapshots.raintank.io/dashboard/snapshot/By0pG1AWnXhbbuHjq5Y1t6mpvrMY03o7)**

## Quick Start (Local Development)

Heimdall is built for a frictionless developer experience. 

**Prerequisites:**
- [Docker](https://docs.docker.com/get-docker/)
- [Go 1.24+](https://golang.org/doc/install)
- [Task](https://taskfile.dev/installation/)
- [buf](https://buf.build/docs/installation)
- [golangci-lint](https://github.com/golangci/golangci-lint)

**Step-by-step:**

1. Clone the repository and enter the project directory.

```bash
git clone https://github.com/rafaeljc/heimdall.git && cd heimdall
```

2. Set up environment variables.

```bash
cp .env.example .env
```

Review the configuration documentation before editing `.env` values: [docs/configuration.md](docs/configuration.md).

3. Generate API key hashes (choose one option).

Option A: Generate a new API key and hash.

```bash
task sec:genkey
```

Option B: Hash an existing API key.

```bash
task sec:hash
```

4. Start local services.

```bash
task dev:up
```

5. After startup, the main local endpoints are:

	- **Control Plane (REST):** http://localhost:8080
	- **API Documentation (Swagger UI):** http://localhost:8081
	- **Data Plane (gRPC):** localhost:50051

	Health check endpoints:
	- **Control Plane:** http://localhost:9090/healthz
	- **Data Plane:** http://localhost:9091/healthz
	- **Syncer:** http://localhost:9092/healthz

6. Stop local services when finished.

```bash
task dev:down
```

Optional: List all available Task commands.

```bash
task --list-all
```

## Infrastructure & Delivery

Operational excellence is baked into Heimdall's design through infrastructure automation, comprehensive validation, and GitOps-powered deployments.

- **Infrastructure as Code (IaC):** The AWS environment (VPC, EKS, Aurora, ElastiCache) is provisioned dynamically using layered **Terraform**.
- **Continuous Integration:** Automated GitHub Actions pipelines enforce code quality across infrastructure and application layers. Infrastructure validation includes Terraform formatting (TFLint), security scanning (Checkov), and Kubernetes manifest validation (Kubeconform). Application validation covers linting and comprehensive test suites. All checks must pass before deployment.
- **Continuous Deployment:** Applications are delivered to Kubernetes using a pull-based **GitOps** model with ArgoCD and Kustomize, enabling automated rollouts, PreSync hook migrations, and zero-downtime updates.

## Roadmap

Upcoming enhancements to the platform's operability:

- [ ]  **Kubernetes Deployment (Interim):** Provide a Kubernetes deployment path (manifests + documented apply flow) while GitOps stabilization is in progress.
- [ ]  **GitOps:** Fix and harden the current GitOps workflow (ArgoCD/Kustomize), then standardize sync policies, promotion flow, and rollback procedures.
- [ ]  **Technical Documentation:** Add C4 Model architecture diagrams, Architectural Decision Records (ADRs), and operational runbooks.
- [ ]  **Management UI:** Build a frontend control panel utilizing React and TypeScript.

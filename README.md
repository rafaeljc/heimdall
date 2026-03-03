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

## Quick Start (Local Development)

Heimdall is built for a frictionless developer experience. 

**Prerequisites:**
- [Docker](https://docs.docker.com/get-docker/)
- [Go 1.24+](https://golang.org/doc/install)
- [Task](https://taskfile.dev/installation/)
- [buf](https://buf.build/docs/installation)
- [golangci-lint](https://github.com/golangci/golangci-lint)

```bash
# 1. Set up environment variables

# Create .env from template
cp .env.example .env

# Generate API key hashes
# Option A: Generate a new API key and hash
# Outputs both the key and SHA-256 hash; update the hashes in .env
task sec:genkey  
# Option B: Hash an existing API key
# Prompts for an API key and outputs its SHA-256 hash; update the hashes in .env
task sec:hash  

# 2. Start local services
task dev:up

# 3. Stop local services
task dev:down
```

## Infrastructure & Delivery

Operational excellence is baked into Heimdall's design through infrastructure automation, comprehensive validation, and GitOps-powered deployments.

- **Infrastructure as Code (IaC):** The AWS environment (VPC, EKS, Aurora, ElastiCache) is provisioned dynamically using layered **Terraform**.
- **Continuous Integration:** Automated GitHub Actions pipelines enforce code quality across infrastructure and application layers. Infrastructure validation includes Terraform formatting (TFLint), security scanning (Checkov), and Kubernetes manifest validation (Kubeconform). Application validation covers linting and comprehensive test suites. All checks must pass before deployment.
- **Continuous Deployment:** Applications are delivered to Kubernetes using a pull-based **GitOps** model with ArgoCD and Kustomize, enabling automated rollouts, PreSync hook migrations, and zero-downtime updates.

## Roadmap

Upcoming enhancements to the platform's operability:

- [ ]  **Observability:** Instrument metrics and distributed tracing using Prometheus and Grafana.
- [ ]  **Performance Tuning:** Implement automated load testing suites using k6 to benchmark Data Plane throughput.
- [ ]  **Technical Documentation:** Add C4 Model architecture diagrams, Architectural Decision Records (ADRs), and operational runbooks.
- [ ]  **Management UI:** Build a frontend control panel utilizing React and TypeScript.

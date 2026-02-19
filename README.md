# 🛡️ Heimdall

**High-Performance, Cloud-Native Feature Flag Management System.**

![Build Status](https://img.shields.io/badge/build-passing-brightgreen) ![Go Report Card](https://img.shields.io/badge/go%20report-A%2B-success) ![License](https://img.shields.io/badge/license-MIT-blue)

> **Note:** This project is a comprehensive engineering case study designed to demonstrate mastery of distributed systems, high-performance API design, and operational excellence in a Cloud Native environment.

## 📖 Overview

Heimdall is a distributed feature flag system designed to decouple code deployment from feature release. It is architected to serve millions of evaluations per second with **sub-millisecond latency**, ensuring that the feature flagging infrastructure never becomes a bottleneck for client applications.

It follows a split-plane architecture to guarantee **High Availability (HA)** and **Resilience**:
* **Control Plane:** A consistent, ACID-compliant REST API for managing flags.
* **Data Plane:** A globally distributed, read-optimized gRPC API for evaluating flags.

## 🏗️ Architecture

Heimdall separates the **Control Plane** (Management) from the **Data Plane** (Evaluation) to ensure that admin-side issues never impact live traffic.

### Key Components

1.  **Control Plane API (REST):** Serves the Admin UI. Writes to **PostgreSQL**. Pushes update jobs to a Redis Queue.
2.  **Syncer (Worker):** An autonomous worker pool that propagates changes from PostgreSQL to Redis using a **competing consumer pattern**. It handles data transformation and triggers cache invalidation.
3.  **Data Plane API (gRPC):** The hot path. Reads pre-compiled rules from **Redis (L2)** and maintains an **in-memory L1 cache**. It uses a custom Rule Engine to evaluate flags in microseconds.

## 🛠️ Tech Stack

* **Language:** Go (Golang)
* **Communication:** gRPC (Protobuf) & REST (Chi)
* **Storage:** PostgreSQL (Source of Truth) & Redis (Cache/PubSub/Queue)
* **Infrastructure:** Docker & Kubernetes (k3s)
* **Observability:** Prometheus & Grafana (OpenTelemetry)
* **Automation:** Taskfile
* **Frontend:** React & TypeScript

## 📂 Repository Structure

This repository follows the **Standard Go Project Layout** and manages multiple services and SDKs in a single source of truth to ensure atomic contract updates.

```text
/heimdall
├── .github/             # CI/CD Pipelines (GitHub Actions)
├── cmd/                 # Main entrypoints for the 3 services
│   ├── heimdall-control # REST API Binary
│   ├── heimdall-data    # gRPC API Binary
│   └── heimdall-syncer  # Worker Binary
├── internal/            # Private application and business logic
├── k8s/                 # Kubernetes manifests for production
├── proto/               # Single Source of Truth for API Contracts
├── sdk/                 # Client SDKs (Go, Python, etc.)
├── tests/               # Load tests (k6) and E2E suites
├── ui/                  # Admin Frontend (React/TypeScript)
├── Taskfile.yml         # Automation runner
├── Dockerfile.*         # Docker build definitions
└── docker-compose.yml   # Local development orchestration
```

## 🚀 Getting Started

### Prerequisites

* [Go 1.24+](https://go.dev/)
* [Docker](https://www.docker.com/) & Docker Compose
* [Task](https://taskfile.dev/)

### Running Locally

To spin up the entire application (Databases + Services + Docs):

```bash
# 1. Clone the repository
git clone https://github.com/rafaeljc/heimdall.git && cd heimdall

# 2. Copy the example environment file and edit the API Key hash values
cp .env.example .env

# 3. Start the environment
task dev:up
```

This will start:

* PostgreSQL (Port 5432)
* Redis (Port 6379)
* Control Plane REST API (Port 8080)
* Data Plane gRPC API (Port 50051)
* Swagger UI (Port 8081)
* Observability:
   * Control Plane (Port 9090)
   * Data Plane (Port 9091)
   * Syncer (Port 9092)

Each observability endpoint exposes liveness, readiness, and metrics endpoints for its respective service.

### Configuration

Heimdall uses environment variables with the `HEIMDALL_` prefix. See **[Configuration Reference](docs/configuration.md)** for complete documentation.

### Development Commands

| Command | Description |
| :--- | :--- |
| **Development** | |
| `task dev:up` | Starts the local environment (builds & runs) |
| `task dev:down` | Stops the environment (preserves data) |
| `task dev:reset` | **Resets** the environment (destroys data volumes) |
| `task dev:logs` | Tails logs from all running services |
| **Dependencies** | |
| `task tidy:deps` | Runs `go mod tidy` forcing Go 1.23 compatibility |
| **Quality & Testing** | |
| `task check:lint` | Runs golangci-lint |
| `task test:unit` | Runs unit tests |
| `task test:integration` | Runs integration tests |
| **Build & Artifacts** | |
| `task build:local` | Builds all Go binaries locally to `/bin` |
| `task build:docker` | Builds Production Docker images |
| **Code Generation** | |
| `task generate:proto` | Generates Go code from `.proto` files |
| **Security** | |
| `task sec:genkey` | Generates a new API key and its SHA-256 hash |
| `task sec:hash` | Computes SHA-256 hash for an existing API key |

## 🧠 Engineering Decisions (ADRs)

This project follows a rigorous design process. All major technical decisions, trade-offs, and alternatives are documented as **Architecture Decision Records (ADRs)**.

| ID | Title | Status | Context |
| :--- | :--- | :--- | :--- |
| **[ADR-001](/)** | Control Plane & Data Plane Separation | 🟢 Accepted | Decouples availability of evaluation from management. |
| **[ADR-002](/)** | Backend Language (Go) | 🟢 Accepted | Chosen for performance, concurrency, and static binaries. |
| **[ADR-003](/)** | Deployment Platform (Kubernetes) | 🟢 Accepted | Production-grade orchestration with k3s. |
| **[ADR-004](/)** | Integration Testing Strategy (Testcontainers) | 🟢 Accepted | Eliminates mocking of databases for higher confidence. |
| **[ADR-005](/)** | Syncer Propagation Strategy | 🔴 Deprecated | Replaced by ADR-007 to fix a race condition. |
| **[ADR-006](/)** | Rule Engine Design | 🟢 Accepted | In-memory, O(1) evaluation logic. |
| **[ADR-007](/)** | Syncer & L1/L2 Cache Invalidation Strategy | 🟢 Accepted | The corrected, race-free propagation architecture. |
| **[ADR-008](/)** | API Protocol (gRPC vs. REST) | 🟢 Accepted | Optimizes for performance (Data) and simplicity (Control). |
| **[ADR-009](/)** | Repository Strategy (Monorepo) | 🟢 Accepted | Ensures atomic contract updates across services and SDKs. |

## 🤝 Contributing

This is a personal portfolio project. Feedback and code reviews are welcome!

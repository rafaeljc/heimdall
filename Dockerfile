# Unified Dockerfile for Heimdall services
# Build any service by specifying the SERVICE_NAME build argument
#
# Usage examples:
#   docker build -t heimdall-control:latest --build-arg SERVICE_NAME=control .
#   docker build -t heimdall-data:latest --build-arg SERVICE_NAME=data .
#   docker build -t heimdall-syncer:latest --build-arg SERVICE_NAME=syncer .

# Stage 1: Build
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy dependency files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments
ARG SERVICE_NAME=control
ARG VERSION=dev

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-s -w \
      -X main.Version=${VERSION} \
      -X runtime/debug.modinfo=booted" \
    -a \
    -trimpath \
    -installsuffix cgo \
    -o /main ./cmd/heimdall-${SERVICE_NAME}

# Stage 2: Run
# Using distroless for production-grade security and minimal attack surface
# Pinned to specific digest for reproducibility and security
#
# Last updated: 2026-02-20
# Check for updates:
#     docker pull gcr.io/distroless/static:nonroot
#     docker inspect --format='{{index .RepoDigests 0}}' gcr.io/distroless/static:nonroot
FROM gcr.io/distroless/static@sha256:01e550fdb7ab79ee7be5ff440a563a58f1fd000ad9e0c532e65c3d23f917f1c5

# Build arguments (needed in second stage)
ARG SERVICE_NAME=control
ARG SERVICE_DESC="heimdall ${SERVICE_NAME}"

# OCI Annotations for cloud environments
LABEL org.opencontainers.image.source="https://github.com/rafaeljc/heimdall" \
      org.opencontainers.image.description="${SERVICE_DESC}" \
      org.opencontainers.image.vendor="heimdall" \
      org.opencontainers.image.service="${SERVICE_NAME}"

# Set working directory
WORKDIR /

# Copy binary from builder
COPY --from=builder --chown=nonroot:nonroot /main /main

# Note: Health checks are handled by Kubernetes probes (livenessProbe, readinessProbe)
# pointing to /healthz and /readyz on port 9090
# For non-Kubernetes deployments, define HEALTHCHECK in docker-compose.yml or similar

# Run as non-root user (built-in to distroless)
USER nonroot

ENTRYPOINT ["/main"]

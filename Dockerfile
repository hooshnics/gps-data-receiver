# syntax=docker/dockerfile:1.4
# Multi-stage build for optimal image size and fast builds

# Stage 1: Build
FROM golang:1.24-alpine AS builder

# Use goproxy.io first; if you get 403 from proxy.golang.org, override with:
#   docker build --build-arg GOPROXY=direct ...
ARG GOPROXY=https://goproxy.io,https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

# Install build dependencies in a single layer
RUN apk add --no-cache git gcc musl-dev

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./

# Download dependencies with BuildKit cache mount (much faster on rebuilds)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Copy source code (this layer will invalidate only when source changes)
COPY . .

# Build with cache mounts and optimizations
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -trimpath \
    -o /app/gps-receiver \
    ./cmd/server

# Stage 2: Build frontend (Vue app for production main domain)
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 3: Runtime (minimal image)
FROM alpine:latest

# Install runtime dependencies (incl. Node.js/npm for tooling or frontend builds) and create user in a single layer
RUN apk --no-cache add ca-certificates tzdata wget nodejs npm && \
    addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/gps-receiver .

# Copy built frontend so main domain (GET /) serves the Vue app
COPY --from=frontend /app/web/dist ./web/dist

# Set ownership and switch user in one step
RUN chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./gps-receiver"]


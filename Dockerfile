# Multi-stage build for optimal image size and fast builds

# Stage 1: Build
FROM golang:1.24-alpine AS builder

# Install build dependencies in a single layer
RUN apk add --no-cache git gcc musl-dev

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code (this layer will invalidate only when source changes)
COPY . .

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -trimpath \
    -o /app/gps-receiver \
    ./cmd/server

# Stage 2: Runtime (minimal image)
FROM alpine:latest

# Install runtime dependencies and create user in a single layer
RUN apk --no-cache add ca-certificates tzdata wget && \
    addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/gps-receiver .

# Set ownership and switch user in one step
RUN chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./gps-receiver"]


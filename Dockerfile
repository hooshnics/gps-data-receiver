# syntax=docker/dockerfile:1.7
# Multi-stage build: separate build toolchains from minimal runtime image.

# Stage 1: Go backend
FROM golang:1.24-alpine AS builder

ARG GOPROXY=https://goproxy.io,https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -trimpath \
    -o /out/gps-receiver \
    ./cmd/server

# Stage 2: Vue frontend (static assets only in final image)
FROM node:20-alpine AS frontend

WORKDIR /app/web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --ignore-scripts

COPY web/ ./
RUN npm run build

# Stage 3: Runtime
FROM alpine:3.21 AS runtime

LABEL org.opencontainers.image.title="gps-data-receiver" \
      org.opencontainers.image.description="GPS data receiver and forwarding service"

RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    wget && \
    addgroup -g 1000 appuser && \
    adduser -D -G appuser -u 1000 appuser

WORKDIR /app

COPY --from=builder --chown=appuser:appuser /out/gps-receiver ./gps-receiver
COPY --from=frontend --chown=appuser:appuser /app/web/dist ./web/dist

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8080/health || exit 1

CMD ["./gps-receiver"]

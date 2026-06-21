# syntax=docker/dockerfile:1.7
# Multi-stage build: Go backend and Vue frontend build in parallel; minimal runtime image.

# Stage 1: Go backend
FROM golang:1.24-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH}

WORKDIR /app

COPY --link go.mod go.sum ./
COPY --link vendor/ ./vendor/

RUN test -f vendor/modules.txt || (echo "ERROR: vendor/ is missing. Run 'make vendor' and commit vendor/ before building." && exit 1)

COPY --link cmd/ ./cmd/
COPY --link internal/ ./internal/
COPY --link pkg/ ./pkg/

RUN --mount=type=cache,target=/root/.cache/go-build \
    go build \
    -mod=vendor \
    -buildvcs=false \
    -ldflags="-w -s" \
    -trimpath \
    -o /out/gps-receiver \
    ./cmd/server

# Stage 2: Vue frontend (uses pre-built web/dist when present; otherwise builds with npm)
FROM node:20-alpine AS frontend

WORKDIR /app/web

ENV CI=true

COPY --link web/package.json web/package-lock.json ./
COPY --link web/src ./src
COPY --link web/public ./public
COPY --link web/index.html web/vite.config.js web/tailwind.config.js web/postcss.config.js ./
COPY --link web/dist ./dist

RUN --mount=type=cache,target=/root/.npm \
    --mount=type=cache,target=/app/web/node_modules \
    if [ ! -f dist/index.html ]; then \
      npm ci --ignore-scripts --no-audit --no-fund && \
      npm run build; \
    else \
      echo "Using pre-built web/dist from build context"; \
    fi

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

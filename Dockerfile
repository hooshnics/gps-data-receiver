# syntax=docker.arvancloud.ir/docker/dockerfile:1.7
# Multi-stage build: Go backend and Vue frontend build in parallel; minimal runtime image.
# Defaults use Arvan Cloud mirrors for environments with limited Docker Hub access (e.g. Iran).

ARG DOCKER_REGISTRY=docker.arvancloud.ir
ARG NPM_REGISTRY=https://registry.npmmirror.com

# Stage 1: Go backend
FROM ${DOCKER_REGISTRY}/golang:1.25-alpine AS builder

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
FROM ${DOCKER_REGISTRY}/node:20-alpine AS frontend

ARG NPM_REGISTRY=https://registry.npmmirror.com

WORKDIR /app/web

ENV CI=true

COPY --link web/package.json web/package-lock.json ./
COPY --link web/src ./src
COPY --link web/public ./public
COPY --link web/index.html web/vite.config.js web/tailwind.config.js web/postcss.config.js ./
COPY --link web/dist ./dist

RUN --mount=type=cache,target=/root/.npm \
    --mount=type=cache,target=/app/web/node_modules \
    npm config set registry "${NPM_REGISTRY}" && \
    if [ ! -f dist/index.html ]; then \
      npm ci --ignore-scripts --no-audit --no-fund && \
      npm run build; \
    else \
      echo "Using pre-built web/dist from build context"; \
    fi

# Stage 3: Runtime
FROM ${DOCKER_REGISTRY}/alpine:3.21 AS runtime

LABEL org.opencontainers.image.title="gps-data-receiver" \
      org.opencontainers.image.description="GPS data receiver and forwarding service"

RUN sed -i 's|https://dl-cdn.alpinelinux.org|https://mirror.arvancloud.ir|g' /etc/apk/repositories && \
    apk add --no-cache \
    ca-certificates \
    tzdata \
    wget \
    su-exec && \
    addgroup -g 1000 appuser && \
    adduser -D -G appuser -u 1000 appuser && \
    mkdir -p /var/lib/gps-archive && \
    chown appuser:appuser /var/lib/gps-archive && \
    chmod 0750 /var/lib/gps-archive

WORKDIR /app

COPY --from=builder --chown=appuser:appuser /out/gps-receiver ./gps-receiver
COPY --from=frontend --chown=appuser:appuser /app/web/dist ./web/dist
COPY --chmod=0755 docker/entrypoint.sh /entrypoint.sh

# Start as root only long enough for entrypoint to chown the archive volume,
# then drop to appuser via su-exec (see docker/entrypoint.sh).
USER root

ENV ARCHIVE_DIR=/var/lib/gps-archive

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/entrypoint.sh"]
CMD ["./gps-receiver"]

.PHONY: help build build-loadtest run test test-unit test-integration test-all benchmark benchmark-handler load-test load-test-high load-test-go jmeter-ingest jmeter-hooshnic jmeter-read jmeter-gui clean fmt lint vendor docker-build docker-build-clean docker-up docker-down docker-logs docker-watch flush-queue flush-database clear-queue clear-database web-install web-build web-dev

# Default target
.DEFAULT_GOAL := help

# Binary name
BINARY_NAME=gps-receiver
BINARY_PATH=./$(BINARY_NAME)

# Docker Compose (V2 plugin)
DOCKER_COMPOSE=docker compose

GOPROXY ?= https://proxy.golang.org,direct
GOSUMDB ?= sum.golang.org
export GOPROXY GOSUMDB

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt

# Redis defaults (override via environment)
REDIS_HOST ?= localhost
REDIS_PORT ?= 6379
REDIS_DB ?= 0

# PostgreSQL defaults (override via environment)
POSTGRES_HOST ?= localhost
POSTGRES_PORT ?= 5432
POSTGRES_USER ?= gps
POSTGRES_PASSWORD ?= gps
POSTGRES_DB ?= gps_receiver

help: ## Display this help message
	@echo "GPS Data Receiver - Makefile Commands"
	@echo "======================================"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the application
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) -o $(BINARY_PATH) -v ./cmd/server
	@echo "Build complete: $(BINARY_PATH)"

web-install: ## Install frontend dependencies (Vue + Tailwind)
	@echo "Installing web dependencies..."
	cd web && npm install
	@echo "Web dependencies installed"

web-build: ## Build frontend for production (output: web/dist)
	@echo "Building frontend..."
	cd web && npm run build
	@echo "Frontend build complete: web/dist"

web-dev: ## Run frontend dev server with hot reload (port 5173, proxies API to 8080)
	cd web && npm run dev

run: ## Run the application
	@echo "Running $(BINARY_NAME)..."
	$(GOCMD) run ./cmd/server/main.go

test: test-unit ## Run unit tests

test-unit: ## Run unit tests
	@echo "Running unit tests..."
	$(GOTEST) -v -race -coverprofile=coverage.txt -covermode=atomic ./tests/unit/...
	@echo "Unit tests complete"

test-integration: ## Run integration tests (requires Redis)
	@echo "Running integration tests..."
	@echo "Note: Redis must be running (docker compose up -d redis)"
	$(GOTEST) -v ./tests/integration/...
	@echo "Integration tests complete"

test-all: ## Run all tests
	@echo "Running all tests..."
	$(GOTEST) -v -race ./tests/...
	@echo "All tests complete"

benchmark: ## Run all Go benchmark tests
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem -benchtime=3s ./tests/benchmark_test.go ./tests/benchmark/...
	@echo "Benchmarks complete"

benchmark-handler: ## Run handler/Redis benchmarks (requires Redis)
	@echo "Running handler benchmarks (requires Redis on localhost:6379)..."
	$(GOTEST) -bench=. -benchmem -benchtime=5s ./tests/benchmark/...
	@echo "Handler benchmarks complete"

build-loadtest: ## Build the high-throughput load test CLI
	@echo "Building loadtest..."
	@mkdir -p bin
	$(GOBUILD) -o bin/loadtest ./cmd/loadtest
	@echo "Build complete: bin/loadtest"

load-test-go: build-loadtest ## Run load test via Go CLI (10K req/s default)
	@./bin/loadtest

load-test-high: build-loadtest ## Run intense load test (10K req/s, 60s)
	@echo "Running high-intensity load test (10K req/s)..."
	TARGET_URL=http://localhost:8080/api/gps/reports \
	DURATION=60s \
	WARMUP=10s \
	RATE=10000 \
	WORKERS=200 \
	./scripts/load_test.sh

coverage: ## Generate test coverage report
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=coverage.out ./tests/unit/...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

clean: ## Clean build artifacts
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -f $(BINARY_PATH)
	rm -f coverage.txt coverage.out coverage.html
	@echo "Clean complete"

fmt: ## Format Go code
	@echo "Formatting code..."
	$(GOFMT) ./...
	@echo "Formatting complete"

lint: ## Run golangci-lint (requires golangci-lint installation)
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with: brew install golangci-lint"; \
		exit 1; \
	fi

tidy: ## Tidy Go modules
	@echo "Tidying Go modules..."
	$(GOMOD) tidy
	@echo "Tidy complete"

vendor: tidy ## Refresh vendored Go dependencies for Docker builds
	@echo "Vendoring Go modules..."
	$(GOMOD) vendor
	@echo "Vendor directory updated"

deps: ## Download Go dependencies
	@echo "Downloading dependencies..."
	$(GOMOD) download
	@echo "Dependencies downloaded"

DOCKER_IMAGE ?= gps-receiver:latest
DOCKER_BUILD_FLAGS ?= --build-arg BUILDKIT_INLINE_CACHE=1

docker-build: web-build ## Build Docker image with BuildKit (builds frontend first)
	@echo "Building Docker image with BuildKit..."
	DOCKER_BUILDKIT=1 docker build $(DOCKER_BUILD_FLAGS) -t $(DOCKER_IMAGE) .
	@echo "Docker image built: $(DOCKER_IMAGE)"

docker-build-clean: web-build ## Build Docker image without cache
	@echo "Building Docker image with BuildKit (no cache)..."
	DOCKER_BUILDKIT=1 docker build --no-cache $(DOCKER_BUILD_FLAGS) -t $(DOCKER_IMAGE) .
	@echo "Docker image built: $(DOCKER_IMAGE)"

docker-up: web-build ## Start all services with Docker Compose (uses BuildKit)
	@echo "Starting services with BuildKit..."
	DOCKER_BUILDKIT=1 $(DOCKER_COMPOSE) up -d --build
	@echo "Services started. Use 'make docker-logs' to view logs"
	@echo "Health check: curl http://localhost:8080/health"

docker-watch: ## Start services with Compose Watch: app rebuilds when Go source, web, or go.mod/go.sum change. Run in foreground.
	DOCKER_BUILDKIT=1 $(DOCKER_COMPOSE) watch

docker-down: ## Stop all services
	@echo "Stopping services..."
	$(DOCKER_COMPOSE) down
	@echo "Services stopped"

docker-down-volumes: ## Stop all services and remove volumes
	@echo "Stopping services and removing volumes..."
	$(DOCKER_COMPOSE) down -v
	@echo "Services stopped and volumes removed"

docker-logs: ## View Docker Compose logs
	$(DOCKER_COMPOSE) logs -f

docker-restart: ## Restart all services
	@echo "Restarting services..."
	$(DOCKER_COMPOSE) restart
	@echo "Services restarted"

docker-ps: ## Show running containers
	$(DOCKER_COMPOSE) ps

grafana-reset-password: ## Reset Grafana admin password (use: make grafana-reset-password NEW_PASSWORD=yournewpassword)
	@if [ -z "$(NEW_PASSWORD)" ]; then \
		echo "Error: Set NEW_PASSWORD. Example: make grafana-reset-password NEW_PASSWORD=mynewpass"; \
		exit 1; \
	fi
	@echo "Resetting Grafana admin password..."
	$(DOCKER_COMPOSE) exec grafana grafana-cli admin reset-admin-password "$(NEW_PASSWORD)"
	@echo "Password reset. Log in at http://localhost:3000 with admin / your new password."

test-prometheus: ## Test Prometheus connectivity
	@echo "Testing Prometheus connectivity..."
	@echo "\n1. Testing from host machine:"
	@curl -s http://localhost:9090/-/healthy && echo "✓ Prometheus is healthy" || echo "✗ Prometheus is not responding"
	@echo "\n2. Testing metrics endpoint from host:"
	@curl -s http://localhost:8080/metrics | head -n 5 && echo "✓ Metrics endpoint accessible" || echo "✗ Metrics endpoint not accessible"
	@echo "\n3. Testing from Prometheus container:"
	@docker exec gps-prometheus wget -q -O- http://app:8080/metrics | head -n 5 && echo "✓ Prometheus can reach app" || echo "✗ Prometheus cannot reach app"
	@echo "\n4. Testing node-exporter from Prometheus container:"
	@docker exec gps-prometheus wget -q -O- http://node-exporter:9100/metrics | head -n 3 && echo "✓ Prometheus can reach node-exporter" || echo "✗ Prometheus cannot reach node-exporter"
	@echo "\n5. Testing cAdvisor from Prometheus container:"
	@docker exec gps-prometheus wget -q -O- http://cadvisor:8080/metrics | head -n 3 && echo "✓ Prometheus can reach cAdvisor" || echo "✗ Prometheus cannot reach cAdvisor"
	@echo "\n6. Checking Prometheus targets:"
	@echo "Visit: http://localhost:9090/targets"

test-grafana: ## Test Grafana and Prometheus datasource
	@echo "Testing Grafana connectivity..."
	@echo "\n1. Testing Grafana health:"
	@curl -s http://localhost:3000/api/health && echo "\n✓ Grafana is healthy" || echo "\n✗ Grafana is not responding"
	@echo "\n2. Testing Grafana datasources:"
	@curl -s -u admin:admin http://localhost:3000/api/datasources && echo "\n✓ Datasources configured" || echo "\n✗ Cannot access datasources"
	@echo "\n3. Testing Prometheus from Grafana container:"
	@docker exec gps-grafana wget -q -O- http://prometheus:9090/-/healthy && echo "✓ Grafana can reach Prometheus" || echo "✗ Grafana cannot reach Prometheus"
	@echo "\nGrafana UI: http://localhost:3000 (admin/admin)"

debug-cors: ## Debug CORS issues between Grafana and Prometheus
	@echo "Debugging CORS issues..."
	@echo "\n1. Checking Prometheus CORS configuration:"
	@docker exec gps-prometheus ps aux | grep prometheus
	@echo "\n2. Testing Prometheus API from Grafana container:"
	@docker exec gps-grafana wget -q -O- --header="Origin: http://localhost:3000" http://prometheus:9090/api/v1/query?query=up 2>&1 | head -n 10
	@echo "\n3. Checking network connectivity:"
	@docker exec gps-grafana ping -c 2 prometheus
	@echo "\n4. Checking Grafana datasource configuration:"
	@docker exec gps-grafana cat /etc/grafana/provisioning/datasources/prometheus.yml

load-test: build-loadtest ## Run load test against running server (default 10K req/s)
	@echo "Running load test..."
	@./scripts/load_test.sh

jmeter-ingest: ## Run JMeter ingest load test (generic JSON, default 1000 req/s)
	@./jmeter/scripts/run-jmeter.sh ingest

jmeter-hooshnic: ## Run JMeter Hooshnic device load test (CSV payloads)
	@./jmeter/scripts/run-jmeter.sh hooshnic

jmeter-read: ## Run JMeter read APIs load test (requires seeded Postgres data)
	@./jmeter/scripts/run-jmeter.sh read

jmeter-gui: ## Open JMeter ingest test plan in GUI for editing
	@./jmeter/scripts/run-jmeter.sh ingest --gui

flush-queue: ## Flush the entire Redis queue
	@echo "Flushing Redis queue..."
	@if $(DOCKER_COMPOSE) ps -q redis >/dev/null 2>&1 && [ -n "$$($(DOCKER_COMPOSE) ps -q redis)" ]; then \
		$(DOCKER_COMPOSE) exec -T redis redis-cli -n $(REDIS_DB) FLUSHDB; \
	elif command -v redis-cli >/dev/null 2>&1; then \
		redis-cli -h $(REDIS_HOST) -p $(REDIS_PORT) -n $(REDIS_DB) FLUSHDB; \
	else \
		echo "Error: redis-cli not found and redis container not running."; \
		echo "Start Redis with 'docker compose up -d redis' or install redis-cli."; \
		exit 1; \
	fi
	@echo "Redis queue flushed successfully."

flush-database: ## Flush the entire PostgreSQL database
	@echo "WARNING: This will drop and recreate the $(POSTGRES_DB) database!"
	@echo "Flushing PostgreSQL database..."
	@if $(DOCKER_COMPOSE) ps -q postgres >/dev/null 2>&1 && [ -n "$$($(DOCKER_COMPOSE) ps -q postgres)" ]; then \
		$(DOCKER_COMPOSE) exec -T postgres psql -U $(POSTGRES_USER) -d postgres -c "DROP DATABASE IF EXISTS $(POSTGRES_DB) WITH (FORCE);" -c "CREATE DATABASE $(POSTGRES_DB);"; \
	elif command -v psql >/dev/null 2>&1; then \
		PGPASSWORD=$(POSTGRES_PASSWORD) psql -h $(POSTGRES_HOST) -p $(POSTGRES_PORT) -U $(POSTGRES_USER) -d postgres -c "DROP DATABASE IF EXISTS $(POSTGRES_DB) WITH (FORCE);" -c "CREATE DATABASE $(POSTGRES_DB);"; \
	else \
		echo "Error: psql not found and postgres container not running."; \
		echo "Start PostgreSQL with 'docker compose up -d postgres' or install the PostgreSQL client."; \
		exit 1; \
	fi
	@echo "PostgreSQL database flushed successfully."

clear-queue: flush-queue ## Alias: clear the Redis queue (same as flush-queue)

clear-database: flush-database ## Alias: clear the PostgreSQL database (same as flush-database)

install-tools: ## Install development tools
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/rakyll/hey@latest
	@if command -v brew >/dev/null 2>&1; then \
		echo "Installing JMeter via Homebrew..."; \
		brew list jmeter >/dev/null 2>&1 || brew install jmeter; \
	else \
		echo "Install JMeter manually: https://jmeter.apache.org/download_jmeter.cgi"; \
	fi
	@echo "Tools installed"

setup: deps ## Setup development environment
	@echo "Setting up development environment..."
	@if [ ! -f .env ]; then \
		cp env.example .env; \
		echo ".env file created. Please edit it with your configuration."; \
	fi
	@echo "Setup complete"

start: docker-up ## Alias for docker-up

stop: docker-down ## Alias for docker-down

status: docker-ps ## Alias for docker-ps

restart: docker-restart ## Alias for docker-restart

all: clean deps fmt test build ## Run all checks and build

.PHONY: all


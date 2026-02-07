.PHONY: help build run test test-unit test-integration test-all benchmark clean fmt lint docker-build docker-up docker-down docker-logs load-test flush-queue flush-database

# Default target
.DEFAULT_GOAL := help

# Binary name
BINARY_NAME=gps-receiver
BINARY_PATH=./$(BINARY_NAME)

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

# MySQL defaults (override via environment)
MYSQL_HOST ?= localhost
MYSQL_PORT ?= 3306
MYSQL_DATABASE ?= gps_receiver

help: ## Display this help message
	@echo "GPS Data Receiver - Makefile Commands"
	@echo "======================================"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the application
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) -o $(BINARY_PATH) -v ./cmd/server
	@echo "Build complete: $(BINARY_PATH)"

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
	@echo "Note: Redis must be running (docker-compose up -d redis)"
	$(GOTEST) -v ./tests/integration/...
	@echo "Integration tests complete"

test-all: ## Run all tests
	@echo "Running all tests..."
	$(GOTEST) -v -race ./tests/...
	@echo "All tests complete"

benchmark: ## Run benchmark tests
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem -benchtime=3s ./tests/
	@echo "Benchmarks complete"

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

deps: ## Download Go dependencies
	@echo "Downloading dependencies..."
	$(GOMOD) download
	@echo "Dependencies downloaded"

docker-build: ## Build Docker image with BuildKit (faster)
	@echo "Building Docker image with BuildKit..."
	DOCKER_BUILDKIT=1 docker build -t gps-receiver:latest .
	@echo "Docker image built: gps-receiver:latest"

docker-up: ## Start all services with docker-compose (uses BuildKit)
	@echo "Starting services with BuildKit..."
	DOCKER_BUILDKIT=1 docker-compose up -d --build
	@echo "Services started. Use 'make docker-logs' to view logs"
	@echo "Health check: curl http://localhost:8080/health"

docker-down: ## Stop all services
	@echo "Stopping services..."
	docker-compose down
	@echo "Services stopped"

docker-down-volumes: ## Stop all services and remove volumes
	@echo "Stopping services and removing volumes..."
	docker-compose down -v
	@echo "Services stopped and volumes removed"

docker-logs: ## View docker-compose logs
	docker-compose logs -f

docker-restart: ## Restart all services
	@echo "Restarting services..."
	docker-compose restart
	@echo "Services restarted"

docker-ps: ## Show running containers
	docker-compose ps

grafana-reset-password: ## Reset Grafana admin password (use: make grafana-reset-password NEW_PASSWORD=yournewpassword)
	@if [ -z "$(NEW_PASSWORD)" ]; then \
		echo "Error: Set NEW_PASSWORD. Example: make grafana-reset-password NEW_PASSWORD=mynewpass"; \
		exit 1; \
	fi
	@echo "Resetting Grafana admin password..."
	docker-compose exec grafana grafana-cli admin reset-admin-password "$(NEW_PASSWORD)"
	@echo "Password reset. Log in at http://localhost:3000 with admin / your new password."

test-prometheus: ## Test Prometheus connectivity
	@echo "Testing Prometheus connectivity..."
	@echo "\n1. Testing from host machine:"
	@curl -s http://localhost:9090/-/healthy && echo "✓ Prometheus is healthy" || echo "✗ Prometheus is not responding"
	@echo "\n2. Testing metrics endpoint from host:"
	@curl -s http://localhost:8080/metrics | head -n 5 && echo "✓ Metrics endpoint accessible" || echo "✗ Metrics endpoint not accessible"
	@echo "\n3. Testing from Prometheus container:"
	@docker exec gps-prometheus wget -q -O- http://app:8080/metrics | head -n 5 && echo "✓ Prometheus can reach app" || echo "✗ Prometheus cannot reach app"
	@echo "\n4. Checking Prometheus targets:"
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

load-test: ## Run load test (requires hey, vegeta, or ab)
	@echo "Running load test..."
	@./scripts/load_test.sh

flush-queue: ## Flush the entire Redis queue
	@echo "Flushing Redis queue..."
	@if docker-compose ps -q redis >/dev/null 2>&1 && [ -n "$$(docker-compose ps -q redis)" ]; then \
		docker-compose exec -T redis redis-cli -n $(REDIS_DB) FLUSHDB; \
	elif command -v redis-cli >/dev/null 2>&1; then \
		redis-cli -h $(REDIS_HOST) -p $(REDIS_PORT) -n $(REDIS_DB) FLUSHDB; \
	else \
		echo "Error: redis-cli not found and redis container not running."; \
		echo "Start Redis with docker-compose or install redis-cli."; \
		exit 1; \
	fi
	@echo "Redis queue flushed successfully."

flush-database: ## Flush the entire MySQL database
	@echo "WARNING: This will drop and recreate the $(MYSQL_DATABASE) database!"
	@echo "Flushing MySQL database..."
	@if docker-compose ps -q mysql >/dev/null 2>&1 && [ -n "$$(docker-compose ps -q mysql)" ]; then \
		docker-compose exec -T mysql mysql -u root -prootpass -e "DROP DATABASE IF EXISTS $(MYSQL_DATABASE); CREATE DATABASE $(MYSQL_DATABASE);"; \
	elif command -v mysql >/dev/null 2>&1; then \
		mysql -h $(MYSQL_HOST) -P $(MYSQL_PORT) -u root -e "DROP DATABASE IF EXISTS $(MYSQL_DATABASE); CREATE DATABASE $(MYSQL_DATABASE);"; \
	else \
		echo "Error: mysql client not found and mysql container not running."; \
		echo "Start MySQL with docker-compose or install mysql client."; \
		exit 1; \
	fi
	@echo "MySQL database flushed successfully."

install-tools: ## Install development tools
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/rakyll/hey@latest
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


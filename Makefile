.PHONY: help build run test test-unit test-integration test-all benchmark clean fmt lint docker-build docker-up docker-down docker-logs load-test

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

load-test: ## Run load test (requires hey, vegeta, or ab)
	@echo "Running load test..."
	@./scripts/load_test.sh

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


# Letraz Job Scraper Makefile

# Variables
BINARY_NAME=letraz-scrapper
MAIN_PATH=cmd/server/main.go
BUILD_DIR=bin

# Colors for output
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[1;33m
BLUE=\033[0;34m
NC=\033[0m # No Color

.PHONY: help dev build clean test lint deps run install hot

# Default target
help: ## Display help information
	@echo "$(BLUE)Letraz Job Scraper - Available Commands$(NC)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "$(GREEN)%-15s$(NC) %s\n", $$1, $$2}'

dev: ## Start development server with hot reload
	@echo "$(YELLOW)🚀 Starting development server...$(NC)"
	@go run $(MAIN_PATH)

build: ## Build the application
	@echo "$(YELLOW)🔨 Building application...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "$(GREEN)✅ Build complete: $(BUILD_DIR)/$(BINARY_NAME)$(NC)"

run: build ## Build and run the application
	@echo "$(YELLOW)🏃 Running application...$(NC)"
	@./$(BUILD_DIR)/$(BINARY_NAME)

install: ## Install dependencies
	@echo "$(YELLOW)📦 Installing dependencies...$(NC)"
	@go mod tidy
	@go mod download
	@echo "$(GREEN)✅ Dependencies installed$(NC)"

test: ## Run tests
	@echo "$(YELLOW)🧪 Running tests...$(NC)"
	@go test -v ./...

test-coverage: ## Run tests with coverage
	@echo "$(YELLOW)🧪 Running tests with coverage...$(NC)"
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)✅ Coverage report generated: coverage.html$(NC)"

lint: ## Run linter
	@echo "$(YELLOW)🔍 Running linter...$(NC)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "$(RED)golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest$(NC)"; \
		go vet ./...; \
	fi

fmt: ## Format code
	@echo "$(YELLOW)💅 Formatting code...$(NC)"
	@go fmt ./...
	@echo "$(GREEN)✅ Code formatted$(NC)"

clean: ## Clean build artifacts
	@echo "$(YELLOW)🧹 Cleaning build artifacts...$(NC)"
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "$(GREEN)✅ Clean complete$(NC)"

deps: ## Download and tidy dependencies
	@echo "$(YELLOW)📥 Downloading dependencies...$(NC)"
	@go mod download
	@go mod tidy
	@echo "$(GREEN)✅ Dependencies updated$(NC)"

# Development tools installation
install-tools: ## Install development tools
	@echo "$(YELLOW)🛠️  Installing development tools...$(NC)"
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/air-verse/air@latest
	@echo "$(GREEN)✅ Development tools installed$(NC)"

setup: ## Setup development environment (adds Go tools to PATH)
	@echo "$(YELLOW)🔧 Setting up development environment...$(NC)"
	@./scripts/setup-env.sh

hot: ## Start development server with hot reload (requires air)
	@echo "$(YELLOW)🔥 Starting hot reload server...$(NC)"
	@GOPATH=$$(go env GOPATH); \
	if command -v air >/dev/null 2>&1; then \
		air; \
	elif [ -f "$$GOPATH/bin/air" ]; then \
		$$GOPATH/bin/air; \
	else \
		echo "$(RED)Air not installed. Install with: make install-tools$(NC)"; \
		echo "$(YELLOW)Falling back to regular dev mode...$(NC)"; \
		make dev; \
	fi

docker-build: ## Build Docker image
	@echo "$(YELLOW)🐳 Building Docker image...$(NC)"
	@docker build -t $(BINARY_NAME):latest .
	@echo "$(GREEN)✅ Docker image built: $(BINARY_NAME):latest$(NC)"

docker-run: ## Run Docker container
	@echo "$(YELLOW)🐳 Running Docker container...$(NC)"
	@docker run -p 8080:8080 --env-file .env $(BINARY_NAME):latest

docker-run-prod: ## Run Docker container in production mode
	@echo "$(YELLOW)🐳 Running Docker container in production mode...$(NC)"
	@docker run -d \
		--name letraz-scrapper-prod \
		--env-file .env \
		-p 8080:8080 \
		--memory=2g \
		--restart unless-stopped \
		--log-driver json-file \
		--log-opt max-size=10m \
		--log-opt max-file=3 \
		$(BINARY_NAME):latest

docker-stop: ## Stop Docker container
	@echo "$(YELLOW)🛑 Stopping Docker container...$(NC)"
	@docker stop letraz-scrapper-prod || true
	@docker rm letraz-scrapper-prod || true

docker-logs: ## View Docker container logs
	@echo "$(YELLOW)📋 Viewing Docker container logs...$(NC)"
	@docker logs -f letraz-scrapper-prod

docker-shell: ## Open shell in Docker container
	@echo "$(YELLOW)🐚 Opening shell in Docker container...$(NC)"
	@docker exec -it letraz-scrapper-prod /bin/sh

docker-clean: ## Clean Docker images and containers
	@echo "$(YELLOW)🧹 Cleaning Docker images and containers...$(NC)"
	@docker system prune -f
	@docker image prune -f

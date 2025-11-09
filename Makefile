# Makefile for Saltbox Docker Controller (SDC)

# Colors for output
GREEN=\033[0;32m
YELLOW=\033[1;33m
NC=\033[0m # No Color

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet
BINARY_NAME=saltbox-docker-controller
BINARY_PATH=./cmd/controller
BUILD_DIR=build
VERSION?=$(shell git describe --tags --always 2>/dev/null || echo "dev")
GIT_COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME?=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Build flags
LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)"
BUILD_FLAGS=CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS)

.PHONY: all build clean test test-short test-coverage deps update tidy modernize fmt vet lint help run-server run-helper version

# Default target
all: test build

## build: Build the saltbox-docker-controller binary
build:
	@echo "$(GREEN)Building $(BINARY_NAME)...$(NC)"
	@echo "Version: $(VERSION)"
	@echo "Commit: $(GIT_COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"
	@mkdir -p $(BUILD_DIR)
	$(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(BINARY_PATH)
	@echo "$(GREEN)Build complete: $(BUILD_DIR)/$(BINARY_NAME)$(NC)"
	@ls -lh $(BUILD_DIR)/$(BINARY_NAME)

## clean: Clean build artifacts and test files
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

## test: Run all tests
test:
	@echo "$(GREEN)Running tests...$(NC)"
	$(GOTEST) -v ./...

## test-short: Run short tests
test-short:
	@echo "Running short tests..."
	$(GOTEST) -short -v ./...

## test-coverage: Run tests with coverage report
test-coverage:
	@echo "$(GREEN)Running tests with coverage...$(NC)"
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@$(GOCMD) tool cover -func=coverage.out | grep total | awk '{print "Total coverage: " $$3}'

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download

## update: Update dependencies to latest versions
update:
	@echo "$(YELLOW)Warning: This will update dependencies. Review changes carefully.$(NC)"
	$(GOGET) -u ./...
	$(GOMOD) tidy

## tidy: Tidy go.mod
tidy:
	@echo "Tidying go.mod..."
	$(GOMOD) tidy

## modernize: Modernize the project (format, vet, update, tidy, and apply latest Go patterns)
modernize: fmt vet update tidy
	@echo "$(GREEN)Running Go modernization tool...$(NC)"
	@$(GOCMD) run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./... || echo "Note: modernize tool completed (warnings are normal)"
	@echo "$(GREEN)Project modernized!$(NC)"

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

## lint: Run golangci-lint (requires golangci-lint to be installed)
lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin"; \
	fi

## run-server: Run the controller in server mode (API server)
run-server:
	@echo "$(GREEN)Running $(BINARY_NAME) in server mode...$(NC)"
	@echo "API will be available at http://127.0.0.1:3377"
	$(GOCMD) run $(BINARY_PATH) server --host 127.0.0.1 --port 3377

## run-helper: Run the controller in helper mode (Docker lifecycle integration)
run-helper:
	@echo "$(GREEN)Running $(BINARY_NAME) in helper mode...$(NC)"
	$(GOCMD) run $(BINARY_PATH) helper --controller-url http://127.0.0.1:3377

## version: Display version information
version:
	@if [ -f $(BUILD_DIR)/$(BINARY_NAME) ]; then \
		$(BUILD_DIR)/$(BINARY_NAME) --version; \
	else \
		echo "Binary not built. Run 'make build' first."; \
	fi

## install: Install the binary (deployment handled by Saltbox Ansible)
install:
	@echo "$(YELLOW)Note: Binary deployment is handled by Saltbox Ansible project.$(NC)"
	@echo "To manually install, copy $(BUILD_DIR)/$(BINARY_NAME) to your desired location."

## help: Show this help message
help:
	@echo "$(GREEN)Saltbox Docker Controller - Makefile$(NC)"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

.PHONY: all build test lint clean run-server install-tools help

# Variables
BINARY_SERVER=queuekitd
BINARY_CLI=queuekit
CMD_SERVER=./cmd/queuekitd
CMD_CLI=./cmd/queuekit
VERSION?=dev
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME?=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)"

# Default target
all: build

# Build both binaries
build: build-server build-cli

# Build server binary
build-server:
	@echo "Building $(BINARY_SERVER)..."
	@go build $(LDFLAGS) -o $(BINARY_SERVER) $(CMD_SERVER)

# Build CLI binary
build-cli:
	@echo "Building $(BINARY_CLI)..."
	@go build $(LDFLAGS) -o $(BINARY_CLI) $(CMD_CLI)

# Run tests
test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...

# Run tests with coverage report
test-coverage: test
	@echo "Generating coverage report..."
	@go tool cover -html=coverage.txt -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run linters
lint:
	@echo "Running linters..."
	@golangci-lint run ./...

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@goimports -w .

# Run go mod tidy
tidy:
	@echo "Tidying go.mod..."
	@go mod tidy

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f $(BINARY_SERVER) $(BINARY_CLI)
	@rm -f coverage.txt coverage.html
	@rm -rf dist/ bin/

# Run server
run-server: build-server
	@echo "Running $(BINARY_SERVER)..."
	@./$(BINARY_SERVER)

# Run CLI
run-cli: build-cli
	@echo "Running $(BINARY_CLI)..."
	@./$(BINARY_CLI)

# Install development tools
install-tools:
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@echo "Tools installed successfully"

# Show help
help:
	@echo "QueueKit - Makefile targets:"
	@echo "  make build          - Build both binaries"
	@echo "  make build-server   - Build queuekitd binary"
	@echo "  make build-cli      - Build queuekit CLI binary"
	@echo "  make test           - Run tests with race detector"
	@echo "  make test-coverage  - Run tests and generate coverage report"
	@echo "  make lint           - Run golangci-lint"
	@echo "  make fmt            - Format code with gofmt and goimports"
	@echo "  make tidy           - Run go mod tidy"
	@echo "  make clean          - Remove build artifacts"
	@echo "  make run-server     - Build and run queuekitd"
	@echo "  make run-cli        - Build and run queuekit CLI"
	@echo "  make install-tools  - Install development tools"
	@echo "  make help           - Show this help message"


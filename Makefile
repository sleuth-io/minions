# Coding Agent Dashboard - Build Configuration

# Variables
APP_NAME := coding-agent-dashboard
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Default target
.PHONY: all
all: build

# Clean build artifacts
.PHONY: clean
clean:
	rm -rf dist/
	rm -rf web-dist/
	rm -f $(APP_NAME)

# Setup development environment
.PHONY: setup
setup:
	@echo "Setting up development environment..."
	@command -v go >/dev/null 2>&1 || { echo "Go is required but not installed. Please install Go 1.21+"; exit 1; }
	@command -v node >/dev/null 2>&1 || { echo "Node.js is required but not installed. Please install Node.js 16+"; exit 1; }
	@command -v npm >/dev/null 2>&1 || { echo "npm is required but not installed. Please install npm"; exit 1; }
	go mod download
	cd web && npm install
	@echo "Development environment setup complete!"

# Install dependencies
.PHONY: deps
deps:
	go mod download
	cd web && npm install

# Build frontend
.PHONY: build-frontend
build-frontend:
	cd web && npm run build

# Build for current platform
.PHONY: build
build: build-frontend
	go build $(LDFLAGS) -o $(APP_NAME) .

# Cross-platform builds
.PHONY: build-all
build-all: build-linux build-darwin build-windows

.PHONY: build-linux
build-linux: build-frontend
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/$(APP_NAME)-linux-amd64 .

.PHONY: build-darwin
build-darwin: build-frontend
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/$(APP_NAME)-darwin-arm64 .

.PHONY: build-windows
build-windows: build-frontend
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/$(APP_NAME)-windows-amd64.exe .

# Development targets
.PHONY: dev
dev:
	go run . --port 8030

.PHONY: dev-frontend
dev-frontend:
	cd web && npm run dev

# Run the built application
.PHONY: run
run: build
	./$(APP_NAME)

# Test targets
.PHONY: test
test:
	go test ./...

.PHONY: test-verbose
test-verbose:
	go test -v ./...

# Lint and format
.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all          - Build for current platform (default)"
	@echo "  setup        - Setup development environment and install dependencies"
	@echo "  build        - Build for current platform"
	@echo "  build-all    - Build for all platforms"
	@echo "  build-linux  - Build for Linux AMD64"
	@echo "  build-darwin - Build for macOS AMD64 and ARM64"
	@echo "  build-windows- Build for Windows AMD64"
	@echo "  clean        - Remove build artifacts"
	@echo "  deps         - Install dependencies"
	@echo "  dev          - Run development server"
	@echo "  dev-frontend - Run frontend development server"
	@echo "  run          - Build and run the application"
	@echo "  test         - Run tests"
	@echo "  fmt          - Format Go code"
	@echo "  vet          - Run Go vet"
	@echo "  help         - Show this help"
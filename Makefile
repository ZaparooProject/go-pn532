.PHONY: all build test lint lint-fix clean coverage check help nfctest readtag

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test

# Default target
all: lint test build

# Build the project
build:
	@echo "Building packages..."
	$(GOBUILD) -v ./...

# Build nfctest binary
nfctest:
	@echo "Building nfctest..."
	$(GOBUILD) -o cmd/nfctest/nfctest ./cmd/nfctest

# Build readtag binary
readtag:
	@echo "Building readtag..."
	$(GOBUILD) -o cmd/readtag/readtag ./cmd/readtag

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -timeout 60s -coverprofile=coverage.txt -covermode=atomic ./...


# Run tests with coverage report
coverage: test
	@echo "Generating coverage report..."
	$(GOCMD) tool cover -html=coverage.txt -o coverage.html
	@echo "Coverage report generated at coverage.html"

# Run linters (includes formatting)
lint:
	@echo "Running linters..."
	$(GOCMD) mod tidy
	golangci-lint run ./...

# Run linters with auto-fix
lint-fix:
	@echo "Running linters with auto-fix..."
	$(GOCMD) mod tidy
	golangci-lint run --fix ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCMD) clean
	rm -f coverage.txt coverage.html
	rm -rf bin/ dist/ build/
	rm -f cmd/nfctest/nfctest cmd/readtag/readtag

# Quick check before committing
check: lint test
	@echo "All checks passed!"

# Show help
help:
	@echo "go-pn532 Makefile"
	@echo "================="
	@echo ""
	@echo "Available targets:"
	@echo "  all            - Lint, test, and build (default)"
	@echo "  build          - Build all packages"
	@echo "  nfctest        - Build nfctest binary to cmd/nfctest/"
	@echo "  readtag        - Build readtag binary to cmd/readtag/"
	@echo "  test           - Run tests with race detector and coverage"
	@echo "  coverage       - Generate HTML coverage report"
	@echo "  lint           - Format code and run linters (golangci-lint)"
	@echo "  lint-fix       - Run linters with auto-fix (golangci-lint --fix)"
	@echo "  clean          - Remove build artifacts"
	@echo "  check          - Run lint and test (pre-commit check)"
	@echo "  help           - Show this help message"
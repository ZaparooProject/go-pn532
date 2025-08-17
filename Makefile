.PHONY: all build test test-unit test-integration lint lint-fix clean coverage coverage-unit coverage-integration check help nfctest readtag tdd

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test

# TDD Guard detection and setup
TDDGUARD_AVAILABLE := $(shell command -v tdd-guard-go 2> /dev/null)
PROJECT_ROOT := $(PWD)

# Conditional test command - pipes through tdd-guard-go if available
ifdef TDDGUARD_AVAILABLE
	GOTEST_WITH_TDD = $(GOTEST) -json ./... 2>&1 | tdd-guard-go -project-root $(PROJECT_ROOT)
else
	GOTEST_WITH_TDD = $(GOTEST)
endif

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

# Run all tests (unit + integration)
test: test-unit test-integration
	@echo "All tests completed!"

# Run unit tests only
test-unit:
	@echo "Running unit tests..."
ifdef TDDGUARD_AVAILABLE
	@echo "TDD Guard detected - integrating test reporting..."
	$(GOTEST) -json -v -race -coverprofile=coverage-unit.txt -covermode=atomic ./... 2>&1 | tdd-guard-go -project-root $(PROJECT_ROOT)
else
	$(GOTEST) -v -race -coverprofile=coverage-unit.txt -covermode=atomic ./...
endif

# Run integration tests only
test-integration:
	@echo "Running integration tests..."
ifdef TDDGUARD_AVAILABLE
	@echo "TDD Guard detected - integrating test reporting..."
	$(GOTEST) -json -v -race -tags=integration -coverprofile=coverage-integration.txt -covermode=atomic ./... 2>&1 | tdd-guard-go -project-root $(PROJECT_ROOT)
else
	$(GOTEST) -v -race -tags=integration -coverprofile=coverage-integration.txt -covermode=atomic ./...
endif

# Run unit tests with coverage report
coverage-unit: test-unit
	@echo "Generating unit test coverage report..."
	$(GOCMD) tool cover -html=coverage-unit.txt -o coverage-unit.html
	@echo "Unit test coverage report generated at coverage-unit.html"

# Run integration tests with coverage report
coverage-integration: test-integration
	@echo "Generating integration test coverage report..."
	$(GOCMD) tool cover -html=coverage-integration.txt -o coverage-integration.html
	@echo "Integration test coverage report generated at coverage-integration.html"

# Run both coverage reports
coverage: coverage-unit coverage-integration
	@echo "All coverage reports generated!"

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

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCMD) clean
	rm -f coverage*.txt coverage*.html
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
	@echo "  all                 - Lint, test, and build (default)"
	@echo "  build               - Build all packages"
	@echo "  nfctest             - Build nfctest binary to cmd/nfctest/"
	@echo "  readtag             - Build readtag binary to cmd/readtag/"
	@echo "  test                - Run all tests (unit + integration)"
	@echo "  test-unit           - Run unit tests only"
	@echo "  test-integration    - Run integration tests only"
	@echo "  coverage            - Generate all HTML coverage reports"
	@echo "  coverage-unit       - Generate unit test coverage report"
	@echo "  coverage-integration - Generate integration test coverage report"
	@echo "  lint                - Format code and run linters (golangci-lint)"
	@echo "  lint-fix            - Run linters with auto-fix (golangci-lint --fix)"
	@echo "  clean               - Remove build artifacts and coverage files"
	@echo "  check               - Run lint and test (pre-commit check)"
	@echo "  help                - Show this help message"
	@echo ""
	@echo "Note: Test commands automatically integrate with tdd-guard-go if available"
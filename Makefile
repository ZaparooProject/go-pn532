.PHONY: all build test test-unit test-integration lint lint-fix clean coverage coverage-unit coverage-integration check help reader tdd

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test

# Package parameter for targeting specific directories
# Usage: make test PKG=./polling
# Usage: make test-unit PKG=./polling/...
# Usage: make test PKG=./cmd/reader
PKG ?= ./...

# TDD Guard detection and setup
TDDGUARD_AVAILABLE := $(shell command -v tdd-guard-go 2> /dev/null)
PROJECT_ROOT := $(PWD)

# Race flag setup - race detector requires CGO, so disable for cross-compilation
ifeq ($(GOOS),)
	# Native build - use race detection
	RACE_FLAG = -race
else
	# Cross-compilation - skip race detection (requires CGO)
	RACE_FLAG = 
endif

# Conditional test command - pipes through tdd-guard-go if available
ifdef TDDGUARD_AVAILABLE
	GOTEST_WITH_TDD = $(GOTEST) -json $(PKG) 2>&1 | tdd-guard-go -project-root $(PROJECT_ROOT)
else
	GOTEST_WITH_TDD = $(GOTEST) $(PKG)
endif

# Default target
all: lint test build

# Build the project
build:
	@echo "Building packages..."
	$(GOBUILD) -v ./...


# Build reader binary
reader:
	@echo "Building reader..."
	$(GOBUILD) -o cmd/reader/reader ./cmd/reader

# Run all tests (unit + integration)
test: test-unit test-integration
	@echo "All tests completed!"

# Run unit tests only
test-unit:
	@echo "Running unit tests on $(PKG)..."
ifdef TDDGUARD_AVAILABLE
	@echo "TDD Guard detected - integrating test reporting..."
	$(GOTEST) -json -v $(RACE_FLAG) -coverprofile=coverage-unit.txt -covermode=atomic $(PKG) 2>&1 | tdd-guard-go -project-root $(PROJECT_ROOT)
else
	$(GOTEST) -v $(RACE_FLAG) -coverprofile=coverage-unit.txt -covermode=atomic $(PKG)
endif

# Run integration tests only
test-integration:
	@echo "Running integration tests on $(PKG)..."
ifdef TDDGUARD_AVAILABLE
	@echo "TDD Guard detected - integrating test reporting..."
	$(GOTEST) -json -v $(RACE_FLAG) -tags=integration -coverprofile=coverage-integration.txt -covermode=atomic $(PKG) 2>&1 | tdd-guard-go -project-root $(PROJECT_ROOT)
else
	$(GOTEST) -v $(RACE_FLAG) -tags=integration -coverprofile=coverage-integration.txt -covermode=atomic $(PKG)
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
	@echo "Running benchmarks on $(PKG)..."
	$(GOTEST) -bench=. -benchmem $(PKG)

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCMD) clean
	rm -f coverage*.txt coverage*.html
	rm -rf bin/ dist/ build/
	rm -f cmd/reader/reader

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
	@echo "  reader              - Build reader binary to cmd/reader/"
	@echo "  test                - Run all tests (unit + integration)"
	@echo "  test-unit           - Run unit tests only"
	@echo "  test-integration    - Run integration tests only"
	@echo "  bench               - Run benchmarks"
	@echo "  coverage            - Generate all HTML coverage reports"
	@echo "  coverage-unit       - Generate unit test coverage report"
	@echo "  coverage-integration - Generate integration test coverage report"
	@echo "  lint                - Format code and run linters (golangci-lint)"
	@echo "  lint-fix            - Run linters with auto-fix (golangci-lint --fix)"
	@echo "  clean               - Remove build artifacts and coverage files"
	@echo "  check               - Run lint and test (pre-commit check)"
	@echo "  help                - Show this help message"
	@echo ""
	@echo "Package targeting (PKG parameter):"
	@echo "  PKG=./...           - Test all packages (default)"
	@echo "  PKG=./polling       - Test specific package"
	@echo "  PKG=./polling/...   - Test package and subpackages"
	@echo "  PKG=./cmd/reader    - Test specific command package"
	@echo ""
	@echo "Examples:"
	@echo "  make test PKG=./polling               - Test polling package only"
	@echo "  make test-unit PKG=./cmd/reader       - Unit tests for reader only"
	@echo "  make bench PKG=./transport            - Benchmark transport package"
	@echo ""
	@echo "Note: Test commands automatically integrate with tdd-guard-go if available"
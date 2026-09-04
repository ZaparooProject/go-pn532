.PHONY: all build test test-unit test-integration deadlock lint lint-fix clean coverage check help reader fuzz fuzz-quick

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

# Build reader binary
reader:
	@echo "Building reader..."
	$(GOBUILD) -o cmd/reader/reader ./cmd/reader

# Run all tests (unit + integration) with race detection
test: test-unit test-integration
	@echo "All tests completed!"

# Run unit tests with race detection
test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v -race -timeout=10m ./...

# Run integration tests with race detection
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -race -timeout=15m -tags=integration ./...

# Run tests with deadlock detection enabled
deadlock:
	@echo "Running tests with deadlock detection..."
	$(GOTEST) -v -race -timeout=10m -tags=deadlock ./...

# Run tests with coverage report
coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.txt -covermode=atomic ./...
	$(GOCMD) tool cover -html=coverage.txt -o coverage.html
	@echo "Coverage report generated at coverage.html"

# Run linters
lint:
	@echo "Running linters..."
	$(GOCMD) mod tidy
	golangci-lint run ./...
	golangci-lint fmt --diff

# Run linters with auto-fix
lint-fix:
	@echo "Running linters with auto-fix..."
	$(GOCMD) mod tidy
	golangci-lint run --fix ./...
	golangci-lint fmt

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Run fuzz tests (30s each - good for development)
fuzz:
	@echo "Running fuzz tests (30s each)..."
	$(GOTEST) -fuzz=FuzzValidateFrameLength -fuzztime=30s ./internal/frame/
	$(GOTEST) -fuzz=FuzzValidateFrameChecksum -fuzztime=30s ./internal/frame/
	$(GOTEST) -fuzz=FuzzExtractFrameData -fuzztime=30s ./internal/frame/
	$(GOTEST) -fuzz=FuzzHandleErrorFrame -fuzztime=30s ./internal/frame/
	$(GOTEST) -fuzz=FuzzCalculateChecksum -fuzztime=30s ./internal/frame/
	$(GOTEST) -fuzz=FuzzBufferPool -fuzztime=30s ./internal/frame/
	@echo "Fuzz testing complete!"

# Run quick fuzz tests (5s each - good for CI)
fuzz-quick:
	@echo "Running quick fuzz tests (5s each)..."
	$(GOTEST) -fuzz=FuzzValidateFrameLength -fuzztime=5s ./internal/frame/
	$(GOTEST) -fuzz=FuzzValidateFrameChecksum -fuzztime=5s ./internal/frame/
	$(GOTEST) -fuzz=FuzzExtractFrameData -fuzztime=5s ./internal/frame/
	$(GOTEST) -fuzz=FuzzHandleErrorFrame -fuzztime=5s ./internal/frame/
	$(GOTEST) -fuzz=FuzzCalculateChecksum -fuzztime=5s ./internal/frame/
	$(GOTEST) -fuzz=FuzzBufferPool -fuzztime=5s ./internal/frame/
	@echo "Quick fuzz testing complete!"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCMD) clean
	rm -f coverage.txt coverage.html
	rm -rf bin/ dist/ build/
	rm -f cmd/reader/reader

# Quick check before committing
check: lint test deadlock
	@echo "All checks passed!"

# Show help
help:
	@echo "go-pn532 Makefile"
	@echo "================="
	@echo ""
	@echo "Available targets:"
	@echo "  all              - Lint, test, and build (default)"
	@echo "  build            - Build all packages"
	@echo "  reader           - Build reader binary to cmd/reader/"
	@echo "  test             - Run all tests (unit + integration) with race detection"
	@echo "  test-unit        - Run unit tests with race detection"
	@echo "  test-integration - Run integration tests with race detection"
	@echo "  deadlock         - Run tests with deadlock detection enabled"
	@echo "  bench            - Run benchmarks"
	@echo "  fuzz             - Run fuzz tests (30s each, ~3 min total)"
	@echo "  fuzz-quick       - Run quick fuzz tests (5s each, ~30s total, for CI)"
	@echo "  coverage         - Run tests and generate HTML coverage report"
	@echo "  lint             - Run linters (golangci-lint)"
	@echo "  lint-fix         - Run linters with auto-fix"
	@echo "  clean            - Remove build artifacts and coverage files"
	@echo "  check            - Run lint, test, and deadlock (pre-commit check)"
	@echo "  help             - Show this help message"

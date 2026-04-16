# Makefile for hexlet-go-crawler
# Provides common development and CI/CD tasks.

.PHONY: build test run lint lint-fix tidy fmt help install-tools

BINARY_NAME=hexlet-go-crawler
CMD_PATH=./cmd/hexlet-go-crawler
MODULE=code

# Default target: show help.
help:
	@echo "Available commands:"
	@echo "  make build          - compile the project"
	@echo "  make test           - run tests with race detector and coverage"
	@echo "  make run URL=<url>  - run crawler against a URL"
	@echo "  make fmt            - format Go source files"
	@echo "  make lint           - run golangci-lint v2"
	@echo "  make lint-fix       - auto-fix fixable lint issues"
	@echo "  make tidy           - tidy go.mod and go.sum"
	@echo "  make install-tools  - install golangci-lint v2"

# Install golangci-lint v2 to GOPATH/bin.
install-tools:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
		sh -s -- -b $$(go env GOPATH)/bin v2.1.0

# Build the binary to bin/hexlet-go-crawler.
build:
	@echo "Building $(BINARY_NAME)..."
	go build -o bin/$(BINARY_NAME) $(CMD_PATH)

# Run tests with race detector and coverage reporting.
test:
	@echo "Running tests..."
	go test -race -cover ./...

# Run the crawler against a provided URL.
# Usage: make run URL=https://example.com
run:
ifndef URL
	@echo "Usage: make run URL=<https://example.com>"
	@echo "Example: make run URL=https://hexlet.io"
else
	@echo "Crawling $(URL)..."
	go run $(CMD_PATH) --depth 1 "$(URL)"
endif

# Format all Go source files using gofmt.
fmt:
	@echo "Formatting Go source files..."
	go fmt ./...

# Run golangci-lint v2 with project configuration.
lint: install-tools
	@echo "Running golangci-lint v2..."
	golangci-lint run ./...

# Auto-fix fixable issues reported by golangci-lint.
lint-fix: install-tools
	@echo "Auto-fixing issues..."
	golangci-lint run --fix ./...

# Clean up go.mod and go.sum by removing unused dependencies.
tidy:
	@echo "Tidying dependencies..."
	go mod tidy

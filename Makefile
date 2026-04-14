.PHONY: build test run lint lint-fix tidy help install-tools

BINARY_NAME=hexlet-go-crawler
CMD_PATH=./cmd/hexlet-go-crawler
MODULE=code

help:
	@echo "Available commands:"
	@echo "  make build          - compile the project"
	@echo "  make test           - run tests with coverage"
	@echo "  make run URL=<url>  - run crawler against URL"
	@echo "  make lint           - run golangci-lint v2"
	@echo "  make lint-fix       - auto-fix fixable issues"
	@echo "  make tidy           - tidy go.mod"
	@echo "  make install-tools  - install golangci-lint v2"

install-tools:
	@echo "Installing golangci-lint v2..."
	@which golangci-lint > /dev/null 2>&1 || \
		(curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v2.0.0)

build:
	@echo "Building $(BINARY_NAME)..."
	go build -o bin/$(BINARY_NAME) $(CMD_PATH)

test:
	@echo "Running tests..."
	go test -race -cover ./...

run:
ifndef URL
	@echo "Usage: make run URL=<https://example.com>"
	@echo "Example: make run URL=https://hexlet.io"
else
	@echo "Crawling $(URL)..."
	go run $(CMD_PATH) --depth 1 "$(URL)"
endif

lint: install-tools
	@echo "Running golangci-lint v2..."
	golangci-lint run ./...

lint-fix: install-tools
	@echo "Auto-fixing issues..."
	golangci-lint run --fix ./...

tidy:
	@echo "Tidying dependencies..."
	go mod tidy

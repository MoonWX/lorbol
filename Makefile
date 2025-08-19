.PHONY: build run test clean fmt vet deps install

BINARY_NAME=lorbol
CLI_BINARY_NAME=lorbol-cli
BUILD_DIR=build

build: deps
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/lorbol
	@go build -o $(BUILD_DIR)/$(CLI_BINARY_NAME) ./cmd/lorbol-cli

run: build
	@echo "Running $(BINARY_NAME)..."
	@./$(BUILD_DIR)/$(BINARY_NAME) -config config.yaml

test:
	@echo "Running tests..."
	@go test -v ./...

clean:
	@echo "Cleaning up..."
	@rm -rf $(BUILD_DIR)
	@rm -rf data/
	@go clean

fmt:
	@echo "Formatting code..."
	@go fmt ./...

vet:
	@echo "Running go vet..."
	@go vet ./...

deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

install: build
	@echo "Installing binaries..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@cp $(BUILD_DIR)/$(CLI_BINARY_NAME) /usr/local/bin/

dev: fmt vet test build

check: fmt vet test

help:
	@echo "Available targets:"
	@echo "  build    - Build the application"
	@echo "  run      - Run the application"
	@echo "  test     - Run tests"
	@echo "  clean    - Clean build artifacts"
	@echo "  fmt      - Format code"
	@echo "  vet      - Run go vet"
	@echo "  deps     - Download dependencies"
	@echo "  install  - Install binaries to /usr/local/bin"
	@echo "  dev      - Run fmt, vet, test, and build"
	@echo "  check    - Run fmt, vet, and test"
	@echo "  help     - Show this help message"
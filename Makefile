# Makefile for shmringbuffer Go package

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOCLEAN=$(GOCMD) clean
GOMOD=$(GOCMD) mod
GOFLAGS=-v # Verbose output for tests, can be customized
TARGET_DIR=./shmringbuffer
TEST_PATTERN=$(TARGET_DIR)
APP_NAME=shmringbuffer_app # Example application name if we build one

# Default target
all: build

# Build the Go package (and its CGo dependencies)
build:
	@echo "Building shmringbuffer package..."
	$(GOBUILD) $(TARGET_DIR)

# Run tests
test:
	@echo "Running shmringbuffer tests..."
	$(GOTEST) $(TEST_PATTERN) $(GOFLAGS)

# Clean build artifacts
clean:
	@echo "Cleaning up build artifacts..."
	$(GOCLEAN) $(TARGET_DIR)
	# Add any other specific cleaning commands here, e.g., removing example binaries
	# rm -f $(APP_NAME)

# Tidy go.mod and go.sum
tidy:
	@echo "Tidying go.mod and go.sum..."
	$(GOMOD) tidy

# Help target to display available commands
help:
	@echo "Available commands:"
	@echo "  make build    - Build the shmringbuffer package"
	@echo "  make test     - Run tests for the shmringbuffer package"
	@echo "  make clean    - Clean build artifacts"
	@echo "  make tidy     - Tidy go.mod and go.sum"
	@echo "  make all      - (Default) Build the package"

# System V IPC Cleanup (informational, requires manual key/ID)
# This is more of a reminder as keys/IDs are dynamic in tests.
# For a fixed application key, this could be more specific.
ipc-list:
	@echo "Listing System V IPC resources (Shared Memory and Semaphores):"
	@ipcs -m
	@ipcs -s
	@echo "Use 'ipcrm -m <id>' or 'ipcrm -s <id>' to remove specific resources."

.PHONY: all build test clean tidy help ipc-list

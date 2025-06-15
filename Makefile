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
# APP_NAME=shmringbuffer_app # Example application name if we build one

# C Compiler parameters
CC=gcc
# CFLAGS for shared library: Position Independent Code, include path for headers
# Assuming all C headers are within TARGET_DIR (shmringbuffer/)
CFLAGS_C_LIB=-fPIC -I$(TARGET_DIR) -Wall -Werror
# LDFLAGS for shared library: link pthreads
LDFLAGS_C_LIB=-lpthread
C_SOURCES=$(TARGET_DIR)/shm_utils.c $(TARGET_DIR)/ringbuffer.c $(TARGET_DIR)/cgo_wrapper.c
SHARED_LIB_NAME=libshmringbuffer.so
SHARED_LIB_PATH=$(TARGET_DIR)/$(SHARED_LIB_NAME)

# Default target
all: build

# Target to build the C shared library
$(SHARED_LIB_PATH): $(C_SOURCES) $(TARGET_DIR)/shm_utils.h $(TARGET_DIR)/ringbuffer.h $(TARGET_DIR)/cgo_wrapper.h $(TARGET_DIR)/shm_ringbuffer.h
	@echo "Building C shared library $(SHARED_LIB_PATH)..."
	$(CC) -shared $(CFLAGS_C_LIB) -o $(SHARED_LIB_PATH) $(C_SOURCES) $(LDFLAGS_C_LIB)

# Build the Go package (and its CGo dependencies)
# This will be updated later to depend on the shared library if needed by CGO linking.
# For now, it just builds Go. If CGO links dynamically, it needs the .so at link and run time.
build: $(SHARED_LIB_PATH) # Ensure C library is built before Go attempts to link
	@echo "Building shmringbuffer Go package (linking against $(SHARED_LIB_PATH))..."
	$(GOBUILD) $(TARGET_DIR)

# Run tests
# Go test will also need to link against the .so file.
# LD_LIBRARY_PATH might need to be set, or rpath used.
test: $(SHARED_LIB_PATH) # Ensure C library is built before testing
	@echo "Running shmringbuffer tests (requires $(SHARED_LIB_PATH) to be found by linker)..."
	# Set LD_LIBRARY_PATH for this command so test executable can find the .so
	LD_LIBRARY_PATH=$(TARGET_DIR):$(LD_LIBRARY_PATH) $(GOTEST) $(TEST_PATTERN) $(GOFLAGS)

# Clean build artifacts
clean:
	@echo "Cleaning up build artifacts..."
	$(GOCLEAN) $(TARGET_DIR)
	rm -f $(SHARED_LIB_PATH)
	# Add any other specific cleaning commands here
	# rm -f $(APP_NAME)

# Tidy go.mod and go.sum
tidy:
	@echo "Tidying go.mod and go.sum..."
	$(GOMOD) tidy

# Help target to display available commands
help:
	@echo "Available commands:"
	@echo "  make all          - (Default) Build C shared library and then the Go package"
	@echo "  make build        - Build C shared library and then the Go package"
	@echo "  make $(SHARED_LIB_PATH) - Build only the C shared library"
	@echo "  make test         - Build C lib (if needed) and run Go tests"
	@echo "  make clean        - Clean build artifacts including the C shared library"
	@echo "  make tidy         - Tidy go.mod and go.sum"
	@echo "  make ipc-list     - List System V IPC resources"


# System V IPC Cleanup (informational, requires manual key/ID)
ipc-list:
	@echo "Listing System V IPC resources (Shared Memory and Semaphores):"
	@ipcs -m
	@ipcs -s
	@echo "Use 'ipcrm -m <id>' or 'ipcrm -s <id>' to remove specific resources."

.PHONY: all build test clean tidy help ipc-list $(SHARED_LIB_PATH)

.PHONY: build run clean test

APP_NAME := trebek
BUILD_DIR := bin
CMD_DIR := cmd/$(APP_NAME)

build:
	@echo "Building $(APP_NAME)..."
	@go build -ldflags="-s -w" -o $(BUILD_DIR)/$(APP_NAME) ./$(CMD_DIR)

run: build
	@echo "Running $(APP_NAME)..."
	@./$(BUILD_DIR)/$(APP_NAME)

clean:
	@echo "Cleaning up..."
	@rm -rf $(BUILD_DIR)

test:
	@echo "Running tests..."
	@go test ./...

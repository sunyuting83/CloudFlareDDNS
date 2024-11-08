APP_NAME = cfddns
SRC = app.go

BUILD_FLAGS = -tags=jsoniter -trimpath -ldflags "-s -w -buildid="

.DEFAULT_GOAL := help

build:
	@echo "Building the application..."
	GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o $(APP_NAME) $(SRC)

build-armv7:
	@echo "Building for ARMv7..."
	GOOS=linux GOARCH=arm GOARM=7 go build $(BUILD_FLAGS) -o $(APP_NAME)_armv7 $(SRC)

build-armv8:
	@echo "Building for ARMv8..."
	GOOS=linux GOARCH=arm64 go build $(BUILD_FLAGS) -o $(APP_NAME)_armv8 $(SRC)

run: build
	@echo "Running the application..."
	./$(APP_NAME)

clean:
	@echo "Cleaning up..."
	go clean
	rm -f $(APP_NAME)

help:
	@echo "Makefile commands:"
	@echo "  build   - Build the application"
	@echo "  build-armv7 - Build the application for ARMv7"
	@echo "  build-armv8 - Build the application for ARMv8"
	@echo "  run     - Build and run the application"
	@echo "  clean   - Clean the build files"
	@echo "  help    - Show this help message"
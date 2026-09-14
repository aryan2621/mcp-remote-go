# Simple MCP Remote Go Makefile
.PHONY: build clean install test build-darwin build-linux build-windows build-all

# Build the binary for current platform
build:
	go build -o bin/mcp-remote-go .

# Build for macOS (Intel and Apple Silicon)
build-darwin:
	@echo "Building for macOS..."
	GOOS=darwin GOARCH=amd64 go build -o bin/mcp-remote-go-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -o bin/mcp-remote-go-darwin-arm64 .
	@echo "macOS builds complete: bin/mcp-remote-go-darwin-amd64, bin/mcp-remote-go-darwin-arm64"

# Build for Linux (Intel and ARM)
build-linux:
	@echo "Building for Linux..."
	GOOS=linux GOARCH=amd64 go build -o bin/mcp-remote-go-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -o bin/mcp-remote-go-linux-arm64 .
	@echo "Linux builds complete: bin/mcp-remote-go-linux-amd64, bin/mcp-remote-go-linux-arm64"

# Build for Windows (Intel and ARM)
build-windows:
	@echo "Building for Windows..."
	GOOS=windows GOARCH=amd64 go build -o bin/mcp-remote-go-windows-amd64.exe .
	GOOS=windows GOARCH=arm64 go build -o bin/mcp-remote-go-windows-arm64.exe .
	@echo "Windows builds complete: bin/mcp-remote-go-windows-amd64.exe, bin/mcp-remote-go-windows-arm64.exe"

# Build for all platforms
build-all: build-darwin build-linux build-windows
	@echo "All platform builds complete!"

# Install to GOPATH/bin
install:
	go install .

# Clean build artifacts
clean:
	rm -rf bin/

# Run tests
test:
	go test ./...

# Run with debug
run:
	go run . $(ARGS)
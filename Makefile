.PHONY: proto proto-lint proto-breaking clean

# Generate code from proto definitions
proto:
	buf generate

# Lint proto files
proto-lint:
	buf lint proto

# Check for breaking changes
proto-breaking:
	buf breaking proto --against '.git#branch=main'

# Clean generated code
clean:
	rm -rf platform/gen
	rm -rf agent/claudecode/src/gen

# Install dependencies for development
setup:
	@echo "Installing buf..."
	@which buf || brew install bufbuild/buf/buf
	@echo "Setting up Go module..."
	cd platform && go mod tidy
	@echo "Setting up TypeScript agent..."
	cd agent/claudecode && bun install

# Run the agent locally (for development)
run-agent:
	cd agent/claudecode && bun run src/index.ts

# Run the platform locally (for development)
run-platform:
	cd platform && go run ./cmd/server

# Build agent container
build-agent:
	docker build -t forge-agent:latest -f agent/claudecode/Dockerfile agent/claudecode

# Build platform container
build-platform:
	docker build -t forge-platform:latest -f platform/Dockerfile platform

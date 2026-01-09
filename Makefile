.PHONY: proto proto-lint proto-breaking clean db-start db-stop db-reset

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

# Container registry configuration
CONTAINER_REGISTRY ?= ghcr.io
CONTAINER_NAMESPACE ?= notzree
AGENT_IMAGE_NAME ?= forge-agent
AGENT_IMAGE_TAG ?= latest

AGENT_IMAGE := $(CONTAINER_REGISTRY)/$(CONTAINER_NAMESPACE)/$(AGENT_IMAGE_NAME):$(AGENT_IMAGE_TAG)

# Build agent container
build-agent:
	docker build -t $(AGENT_IMAGE) -f agent/claudecode/Dockerfile agent/claudecode
	@echo "Built image: $(AGENT_IMAGE)"

# Build agent with git sha tag
build-agent-sha:
	$(eval GIT_SHA := $(shell git rev-parse --short HEAD))
	docker build -t $(CONTAINER_REGISTRY)/$(CONTAINER_NAMESPACE)/$(AGENT_IMAGE_NAME):$(GIT_SHA) -f agent/claudecode/Dockerfile agent/claudecode
	@echo "Built image: $(CONTAINER_REGISTRY)/$(CONTAINER_NAMESPACE)/$(AGENT_IMAGE_NAME):$(GIT_SHA)"

# Push agent container to registry
push-agent:
	docker push $(AGENT_IMAGE)
	@echo "Pushed image: $(AGENT_IMAGE)"

# Push agent with git sha tag
push-agent-sha:
	$(eval GIT_SHA := $(shell git rev-parse --short HEAD))
	docker push $(CONTAINER_REGISTRY)/$(CONTAINER_NAMESPACE)/$(AGENT_IMAGE_NAME):$(GIT_SHA)
	@echo "Pushed image: $(CONTAINER_REGISTRY)/$(CONTAINER_NAMESPACE)/$(AGENT_IMAGE_NAME):$(GIT_SHA)"

# Build and push agent (convenience target)
release-agent: build-agent push-agent
	@echo "Released $(AGENT_IMAGE)"

# Build and push agent with git sha (for CI)
release-agent-sha: build-agent-sha push-agent-sha
	$(eval GIT_SHA := $(shell git rev-parse --short HEAD))
	@echo "Released $(CONTAINER_REGISTRY)/$(CONTAINER_NAMESPACE)/$(AGENT_IMAGE_NAME):$(GIT_SHA)"

# Login to container registry (for GHCR, use: echo $GITHUB_TOKEN | make registry-login)
registry-login:
	@echo "Logging into $(CONTAINER_REGISTRY)..."
	@docker login $(CONTAINER_REGISTRY)

# Build platform container
build-platform:
	docker build -t forge-platform:latest -f platform/Dockerfile platform

# Database configuration
DB_CONTAINER_NAME := forge-postgres
DB_USER := forge
DB_PASSWORD := forgedev
DB_NAME := forge
DB_PORT := 5432

# Start PostgreSQL container for development
db-start:
	@echo "Starting PostgreSQL container..."
	@docker run -d \
		--name $(DB_CONTAINER_NAME) \
		-e POSTGRES_USER=$(DB_USER) \
		-e POSTGRES_PASSWORD=$(DB_PASSWORD) \
		-e POSTGRES_DB=$(DB_NAME) \
		-p $(DB_PORT):5432 \
		-v forge-postgres-data:/var/lib/postgresql/data \
		postgres:16-alpine || docker start $(DB_CONTAINER_NAME)
	@echo "PostgreSQL is running on localhost:$(DB_PORT)"
	@echo "  User: $(DB_USER)"
	@echo "  Password: $(DB_PASSWORD)"
	@echo "  Database: $(DB_NAME)"

# Stop PostgreSQL container
db-stop:
	@echo "Stopping PostgreSQL container..."
	@docker stop $(DB_CONTAINER_NAME) || true

# Reset PostgreSQL (stop, remove container and volume, then start fresh)
db-reset:
	@echo "Resetting PostgreSQL..."
	@docker stop $(DB_CONTAINER_NAME) || true
	@docker rm $(DB_CONTAINER_NAME) || true
	@docker volume rm forge-postgres-data || true
	@$(MAKE) db-start

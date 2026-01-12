# Forge Development Commands
# Run `just --list` to see all available commands

set dotenv-load := true
set shell := ["bash", "-cu"]

# Default recipe to list all commands
default:
    @just --list

# =============================================================================
# Variables
# =============================================================================

container_registry := env("CONTAINER_REGISTRY", "ghcr.io")
container_namespace := env("CONTAINER_NAMESPACE", "notzree")
agent_image_name := env("AGENT_IMAGE_NAME", "forge-agent")
agent_image_tag := env("AGENT_IMAGE_TAG", "latest")
agent_image := container_registry + "/" + container_namespace + "/" + agent_image_name + ":" + agent_image_tag

# =============================================================================
# Development
# =============================================================================

# Start local development environment with k3d
dev:
    ./scripts/dev.sh

# Start local development environment and rebuild agent image
dev-rebuild:
    ./scripts/dev.sh --rebuild

# Setup k3d cluster only (without starting platform)
k3d-setup:
    ./scripts/k3d-setup.sh

# Delete k3d cluster
k3d-delete:
    k3d cluster delete forge-dev
    k3d registry delete forge-registry.localhost || true

# =============================================================================
# Proto Generation
# =============================================================================

# Generate code from proto definitions
proto:
    ./scripts/proto.sh generate

# Lint proto files
proto-lint:
    ./scripts/proto.sh lint

# Check for breaking changes
proto-breaking:
    ./scripts/proto.sh breaking

# Clean generated code
clean:
    ./scripts/proto.sh clean

# =============================================================================
# Setup
# =============================================================================

# Install dependencies for development
setup:
    ./scripts/setup.sh

# =============================================================================
# Local Development (without k3d)
# =============================================================================

# Run the agent locally (for development)
run-agent:
    cd agent/claudecode && bun run src/index.ts

# Run the platform locally (for development)
run-platform:
    cd platform && go run ./cmd/server

# =============================================================================
# Container Builds
# =============================================================================

# Build agent container
build-agent:
    ./scripts/build-agent.sh

# Build agent with git sha tag
build-agent-sha:
    ./scripts/build-agent.sh --sha

# Push agent container to registry
push-agent:
    docker push {{ agent_image }}
    @echo "Pushed image: {{ agent_image }}"

# Push agent with git sha tag
push-agent-sha:
    #!/usr/bin/env bash
    GIT_SHA=$(git rev-parse --short HEAD)
    docker push {{ container_registry }}/{{ container_namespace }}/{{ agent_image_name }}:${GIT_SHA}
    echo "Pushed image: {{ container_registry }}/{{ container_namespace }}/{{ agent_image_name }}:${GIT_SHA}"

# Build and push agent (convenience target)
release-agent:
    ./scripts/build-agent.sh --push

# Build and push agent with git sha (for CI)
release-agent-sha:
    ./scripts/build-agent.sh --sha --push

# Login to container registry
registry-login:
    @echo "Logging into {{ container_registry }}..."
    @docker login {{ container_registry }}

# Build platform container
build-platform:
    docker build -t forge-platform:latest -f platform/Dockerfile platform

# =============================================================================
# Database Migrations (Goose)
# =============================================================================

# Internal: Get database URL based on environment
[private]
_db_url env:
    #!/usr/bin/env bash
    if [ "{{ env }}" = "prod" ]; then
        echo "${DATABASE_URL_PROD}"
    else
        echo "${DATABASE_URL_DEV}"
    fi

# Run goose migrations (usage: just g up --env=dev)
[no-cd]
g command *args:
    #!/usr/bin/env bash
    set -euo pipefail

    # Parse --env flag from args
    ENV="dev"
    GOOSE_ARGS=""
    for arg in {{ args }}; do
        if [[ "$arg" == --env=* ]]; then
            ENV="${arg#--env=}"
        else
            GOOSE_ARGS="$GOOSE_ARGS $arg"
        fi
    done

    # Get database URL based on environment
    if [ "$ENV" = "prod" ]; then
        DB_URL="${DATABASE_URL_PROD:-}"
        if [ -z "$DB_URL" ]; then
            echo "Error: DATABASE_URL_PROD is not set"
            exit 1
        fi
    else
        DB_URL="${DATABASE_URL_DEV:-}"
        if [ -z "$DB_URL" ]; then
            echo "Error: DATABASE_URL_DEV is not set"
            exit 1
        fi
    fi

    MIGRATIONS_DIR="platform/internal/sqlc/migrations"

    echo "Using $ENV database..."

    case "{{ command }}" in
        create)
            # Force SQL migrations: -s for sequential versioning, sql at end for SQL type
            goose -s -dir "$MIGRATIONS_DIR" create $GOOSE_ARGS sql
            ;;
        up|down|status|version|redo|reset)
            goose -dir "$MIGRATIONS_DIR" postgres "$DB_URL" {{ command }} $GOOSE_ARGS
            ;;
        up-one)
            goose -dir "$MIGRATIONS_DIR" postgres "$DB_URL" up-by-one $GOOSE_ARGS
            ;;
        down-one)
            goose -dir "$MIGRATIONS_DIR" postgres "$DB_URL" down $GOOSE_ARGS
            ;;
        *)
            echo "Unknown command: {{ command }}"
            echo ""
            echo "Available commands:"
            echo "  just g create <name> --env=dev|prod   Create a new SQL migration"
            echo "  just g up --env=dev|prod              Run all pending migrations"
            echo "  just g down --env=dev|prod            Roll back the last migration"
            echo "  just g status --env=dev|prod          Show migration status"
            echo "  just g version --env=dev|prod         Show current migration version"
            echo "  just g redo --env=dev|prod            Roll back and re-run the last migration"
            echo "  just g reset --env=dev|prod           Roll back all migrations"
            echo "  just g up-one --env=dev|prod          Run the next pending migration"
            echo "  just g down-one --env=dev|prod        Roll back exactly one migration"
            echo ""
            echo "Default environment is 'dev' if --env is not specified"
            exit 1
            ;;
    esac

# Shorthand: Run migrations up on dev
migrate-dev:
    just g up --env=dev

# Shorthand: Run migrations up on prod
migrate-prod:
    just g up --env=prod

# Shorthand: Check migration status on dev
migrate-status:
    just g status --env=dev

# =============================================================================
# Local Postgres (Docker)
# =============================================================================

pg_container := "forge-postgres"
pg_port := env("POSTGRES_PORT", "5432")
pg_user := env("POSTGRES_USER", "forge")
pg_password := env("POSTGRES_PASSWORD", "forge")
pg_db := env("POSTGRES_DB", "forge_dev")

# Start local postgres container
pg-up:
    #!/usr/bin/env bash
    set -euo pipefail
    if docker ps -a --format '{{{{.Names}}' | grep -q "^{{ pg_container }}$"; then
        echo "Container {{ pg_container }} exists, starting..."
        docker start {{ pg_container }}
    else
        echo "Creating and starting {{ pg_container }}..."
        docker run -d \
            --name {{ pg_container }} \
            -e POSTGRES_USER={{ pg_user }} \
            -e POSTGRES_PASSWORD={{ pg_password }} \
            -e POSTGRES_DB={{ pg_db }} \
            -p {{ pg_port }}:5432 \
            -v forge-postgres-data:/var/lib/postgresql/data \
            postgres:16-alpine
    fi
    echo "Postgres running on localhost:{{ pg_port }}"
    echo "Connection: postgres://{{ pg_user }}:{{ pg_password }}@localhost:{{ pg_port }}/{{ pg_db }}?sslmode=disable"

# Stop local postgres container
pg-stop:
    docker stop {{ pg_container }}
    @echo "Postgres stopped"

# Remove local postgres container (keeps data volume)
pg-rm:
    docker rm -f {{ pg_container }} || true
    @echo "Postgres container removed (data volume preserved)"

# Remove local postgres container and data volume
pg-nuke:
    docker rm -f {{ pg_container }} || true
    docker volume rm forge-postgres-data || true
    @echo "Postgres container and data volume removed"

# Show postgres container status
pg-status:
    @docker ps -a --filter "name={{ pg_container }}" --format "table {{{{.Names}}\t{{{{.Status}}\t{{{{.Ports}}"

# Connect to postgres via psql
pg-shell:
    docker exec -it {{ pg_container }} psql -U {{ pg_user }} -d {{ pg_db }}

# View postgres logs
pg-logs:
    docker logs -f {{ pg_container }}

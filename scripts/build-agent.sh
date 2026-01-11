#!/usr/bin/env bash
set -euo pipefail

# Build agent Docker image
# Usage: ./scripts/build-agent.sh [--sha] [--push]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"

# Container registry configuration (can be overridden via environment)
CONTAINER_REGISTRY="${CONTAINER_REGISTRY:-ghcr.io}"
CONTAINER_NAMESPACE="${CONTAINER_NAMESPACE:-notzree}"
AGENT_IMAGE_NAME="${AGENT_IMAGE_NAME:-forge-agent}"
AGENT_IMAGE_TAG="${AGENT_IMAGE_TAG:-latest}"

# Parse arguments
USE_SHA=false
PUSH=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --sha)
            USE_SHA=true
            shift
            ;;
        --push)
            PUSH=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [--sha] [--push]"
            echo ""
            echo "Options:"
            echo "  --sha     Use git SHA as image tag instead of 'latest'"
            echo "  --push    Push image to registry after building"
            echo ""
            echo "Environment variables:"
            echo "  CONTAINER_REGISTRY   Registry host (default: ghcr.io)"
            echo "  CONTAINER_NAMESPACE  Registry namespace (default: notzree)"
            echo "  AGENT_IMAGE_NAME     Image name (default: forge-agent)"
            echo "  AGENT_IMAGE_TAG      Image tag (default: latest)"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Determine tag
if [[ "${USE_SHA}" == "true" ]]; then
    AGENT_IMAGE_TAG=$(git rev-parse --short HEAD)
fi

AGENT_IMAGE="${CONTAINER_REGISTRY}/${CONTAINER_NAMESPACE}/${AGENT_IMAGE_NAME}:${AGENT_IMAGE_TAG}"

echo "Building agent image: ${AGENT_IMAGE}"

docker build \
    -t "${AGENT_IMAGE}" \
    -f "${PROJECT_ROOT}/agent/claudecode/Dockerfile" \
    "${PROJECT_ROOT}/agent/claudecode"

echo "Built image: ${AGENT_IMAGE}"

if [[ "${PUSH}" == "true" ]]; then
    echo "Pushing image: ${AGENT_IMAGE}"
    docker push "${AGENT_IMAGE}"
    echo "Pushed image: ${AGENT_IMAGE}"
fi

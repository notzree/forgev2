#!/usr/bin/env bash
set -euo pipefail

# Setup script for development dependencies
# Usage: ./scripts/setup.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"

echo "Installing buf..."
which buf || brew install bufbuild/buf/buf

echo "Setting up Go module..."
cd "${PROJECT_ROOT}/platform" && go mod tidy

echo "Setting up TypeScript agent..."
cd "${PROJECT_ROOT}/agent/claudecode" && bun install

echo ""
echo "Setup complete!"

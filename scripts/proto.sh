#!/usr/bin/env bash
set -euo pipefail

# Protocol buffer generation script
# Usage: ./scripts/proto.sh [lint|breaking|clean]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"

cd "${PROJECT_ROOT}"

case "${1:-generate}" in
    generate)
        echo "Generating code from proto definitions..."
        buf generate
        echo "Done."
        ;;
    lint)
        echo "Linting proto files..."
        buf lint proto
        echo "Done."
        ;;
    breaking)
        echo "Checking for breaking changes..."
        buf breaking proto --against '.git#branch=main'
        echo "Done."
        ;;
    clean)
        echo "Cleaning generated code..."
        rm -rf platform/gen
        rm -rf agent/claudecode/src/gen
        echo "Done."
        ;;
    *)
        echo "Usage: $0 [generate|lint|breaking|clean]"
        exit 1
        ;;
esac

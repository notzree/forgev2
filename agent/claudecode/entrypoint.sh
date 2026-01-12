#!/bin/bash
set -e

# Default provider is opencode
PROVIDER="${AGENT_PROVIDER:-opencode}"

echo "Starting agent with provider: $PROVIDER"

# Debug: show PATH and check for binaries
echo "PATH: $PATH"
echo "Checking for opencode binary..."
which opencode || echo "opencode not found in PATH"
ls -la /home/agent/.opencode/bin/ 2>/dev/null || echo "OpenCode bin directory not found"

if [ "$PROVIDER" = "opencode" ]; then
    # Start OpenCode server in the background
    OPENCODE_PORT="${OPENCODE_PORT:-4096}"
    echo "Starting OpenCode server on port $OPENCODE_PORT..."

    # Start opencode serve in background
    /home/agent/.opencode/bin/opencode serve --port "$OPENCODE_PORT" --hostname 0.0.0.0 &
    OPENCODE_PID=$!

    # Wait for OpenCode server to be ready
    echo "Waiting for OpenCode server to be ready..."
    for i in {1..30}; do
        # Try the /global/health endpoint (from OpenCode API docs)
        if curl -s "http://localhost:$OPENCODE_PORT/global/health" > /dev/null 2>&1; then
            echo "OpenCode server is ready"
            break
        fi
        if [ $i -eq 30 ]; then
            echo "Warning: OpenCode server health check timed out, continuing anyway..."
        fi
        sleep 1
    done

    # Export the base URL for the agent
    export OPENCODE_BASE_URL="http://localhost:$OPENCODE_PORT"
fi

# Start the agent
echo "Starting agent..."
exec bun run src/index.ts

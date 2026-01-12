# syntax=docker/dockerfile:1

# Build stage
FROM oven/bun:1 AS builder

WORKDIR /app

# Copy package files first for better layer caching
COPY package.json bun.lockb ./

# Install dependencies
RUN bun install --frozen-lockfile

# Copy source code
COPY tsconfig.json ./
COPY src ./src

# Production stage
FROM oven/bun:1-slim AS production

WORKDIR /app

# Install required packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    ca-certificates \
    git \
    wget \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user for security
RUN groupadd --system --gid 1001 forge && \
    useradd --system --uid 1001 --gid forge --shell /bin/bash --create-home agent && \
    mkdir -p /home/agent/workspace && \
    chown -R agent:forge /home/agent/workspace

# Copy only necessary files from builder
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package.json ./
COPY --from=builder /app/src ./src

# Set ownership
RUN chown -R agent:forge /app

# Switch to non-root user
USER agent

# Install Claude Code CLI as the agent user (for claude provider)
RUN curl -fsSL https://claude.ai/install.sh | bash

# Install OpenCode CLI as the agent user (for opencode provider)
# OpenCode installs to $HOME/.opencode/bin by default
RUN curl -fsSL https://opencode.ai/install | bash

# Add CLIs to PATH
# - Claude Code: ~/.claude/local/bin
# - OpenCode: ~/.opencode/bin
ENV PATH="/home/agent/.claude/local/bin:/home/agent/.opencode/bin:${PATH}"

# Default port (can be overridden via PORT env var)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT:-8080}/healthz || exit 1

# Copy entrypoint script
COPY --chown=agent:forge entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Start via entrypoint
CMD ["/app/entrypoint.sh"]

-- +goose Up

-- Webhook delivery tracking for retry and circuit breaker logic
CREATE TABLE webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    webhook_url TEXT NOT NULL,
    webhook_secret_hash TEXT, -- SHA256 of secret for verification

    -- Delivery state
    seq BIGINT NOT NULL DEFAULT 0,
    last_event_type TEXT,
    status TEXT NOT NULL DEFAULT 'pending', -- pending, delivering, completed, failed

    -- Retry tracking
    attempt_count INT NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    next_retry_at TIMESTAMPTZ,
    last_error TEXT,

    -- Circuit breaker state (per webhook_url)
    consecutive_failures INT NOT NULL DEFAULT 0,
    circuit_open_until TIMESTAMPTZ,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,

    UNIQUE(request_id)
);

CREATE INDEX idx_webhook_deliveries_agent ON webhook_deliveries(agent_id);
CREATE INDEX idx_webhook_deliveries_status ON webhook_deliveries(status) WHERE status != 'completed';
CREATE INDEX idx_webhook_deliveries_retry ON webhook_deliveries(next_retry_at) WHERE status = 'pending';

-- +goose Down

DROP INDEX IF EXISTS idx_webhook_deliveries_retry;
DROP INDEX IF EXISTS idx_webhook_deliveries_status;
DROP INDEX IF EXISTS idx_webhook_deliveries_agent;
DROP TABLE IF EXISTS webhook_deliveries;

-- name: CreateWebhookDelivery :one
INSERT INTO webhook_deliveries (
    request_id, agent_id, webhook_url, webhook_secret_hash
) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_deliveries WHERE request_id = $1;

-- name: GetWebhookDeliveryByID :one
SELECT * FROM webhook_deliveries WHERE id = $1;

-- name: UpdateDeliveryStatus :exec
UPDATE webhook_deliveries
SET status = $2, updated_at = NOW()
WHERE request_id = $1;

-- name: UpdateDeliverySeq :exec
UPDATE webhook_deliveries
SET seq = $2, last_event_type = $3, updated_at = NOW()
WHERE request_id = $1;

-- name: MarkDeliveryCompleted :exec
UPDATE webhook_deliveries
SET status = 'completed', completed_at = NOW(), updated_at = NOW(), consecutive_failures = 0
WHERE request_id = $1;

-- name: MarkDeliveryFailed :exec
UPDATE webhook_deliveries
SET status = 'failed', updated_at = NOW()
WHERE request_id = $1;

-- name: RecordDeliveryAttempt :exec
UPDATE webhook_deliveries
SET attempt_count = attempt_count + 1,
    last_attempt_at = NOW(),
    updated_at = NOW()
WHERE request_id = $1;

-- name: RecordDeliveryFailure :exec
UPDATE webhook_deliveries
SET last_error = $2,
    consecutive_failures = consecutive_failures + 1,
    next_retry_at = $3,
    updated_at = NOW()
WHERE request_id = $1;

-- name: RecordDeliverySuccess :exec
UPDATE webhook_deliveries
SET consecutive_failures = 0,
    last_error = NULL,
    next_retry_at = NULL,
    updated_at = NOW()
WHERE request_id = $1;

-- name: OpenCircuitForURL :exec
UPDATE webhook_deliveries
SET circuit_open_until = $2, updated_at = NOW()
WHERE webhook_url = $1 AND (circuit_open_until IS NULL OR circuit_open_until < NOW());

-- name: CloseCircuitForURL :exec
UPDATE webhook_deliveries
SET circuit_open_until = NULL, consecutive_failures = 0, updated_at = NOW()
WHERE webhook_url = $1;

-- name: GetPendingRetries :many
SELECT * FROM webhook_deliveries
WHERE status = 'pending'
  AND next_retry_at IS NOT NULL
  AND next_retry_at <= NOW()
  AND (circuit_open_until IS NULL OR circuit_open_until <= NOW())
ORDER BY next_retry_at
LIMIT $1;

-- name: GetActiveDeliveriesForAgent :many
SELECT * FROM webhook_deliveries
WHERE agent_id = $1
  AND status IN ('pending', 'delivering')
ORDER BY created_at DESC;

-- name: IsCircuitOpen :one
SELECT EXISTS (
    SELECT 1 FROM webhook_deliveries
    WHERE webhook_url = $1
      AND circuit_open_until IS NOT NULL
      AND circuit_open_until > NOW()
) AS is_open;

-- name: GetConsecutiveFailures :one
SELECT COALESCE(MAX(consecutive_failures), 0)::int AS failures
FROM webhook_deliveries
WHERE webhook_url = $1
  AND status IN ('pending', 'delivering');

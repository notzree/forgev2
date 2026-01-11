package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/forge/platform/internal/config"
	"github.com/forge/platform/internal/sqlc/gen"
)

// RetryDelays defines the delay before each retry attempt
var RetryDelays = []time.Duration{
	0,                // Attempt 1: immediate
	1 * time.Second,  // Attempt 2: 1s
	5 * time.Second,  // Attempt 3: 5s
	30 * time.Second, // Attempt 4: 30s
	60 * time.Second, // Attempt 5: 60s
}

// DeliveryService handles webhook delivery with retries and circuit breaker
type DeliveryService struct {
	client     *http.Client
	logger     *zap.Logger
	queries    *sqlc.Queries
	pool       *pgxpool.Pool
	cfg        *config.Config

	// Circuit breaker state (in-memory, per webhook URL)
	circuitMu     sync.RWMutex
	circuitStates map[string]*circuitState
}

type circuitState struct {
	failures   int
	openUntil  time.Time
	lastFailed time.Time
}

// NewDeliveryService creates a new webhook delivery service
func NewDeliveryService(pool *pgxpool.Pool, cfg *config.Config, logger *zap.Logger) *DeliveryService {
	return &DeliveryService{
		client: &http.Client{
			Timeout: cfg.WebhookTimeout,
		},
		logger:        logger,
		queries:       sqlc.New(pool),
		pool:          pool,
		cfg:           cfg,
		circuitStates: make(map[string]*circuitState),
	}
}

// Deliver sends a webhook payload synchronously with retries
func (s *DeliveryService) Deliver(ctx context.Context, webhookCfg Config, payload Payload) error {
	// Check circuit breaker
	if s.isCircuitOpen(webhookCfg.URL) {
		s.logger.Warn("circuit breaker open, skipping delivery",
			zap.String("webhook_url", webhookCfg.URL),
			zap.String("request_id", payload.RequestID),
		)
		return fmt.Errorf("circuit breaker open for %s", webhookCfg.URL)
	}

	var lastErr error
	maxRetries := min(s.cfg.WebhookMaxRetries, len(RetryDelays))

	for attempt := range maxRetries {
		if attempt > 0 {
			delay := RetryDelays[attempt]
			s.logger.Debug("retrying webhook delivery",
				zap.Int("attempt", attempt+1),
				zap.Duration("delay", delay),
				zap.String("request_id", payload.RequestID),
			)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		result := s.deliverOnce(ctx, webhookCfg, payload)
		if result.Success {
			s.recordSuccess(webhookCfg.URL)
			return nil
		}

		lastErr = result.Error
		s.recordFailure(webhookCfg.URL, result.Error)

		// Don't retry on 4xx errors (client error)
		if result.StatusCode >= 400 && result.StatusCode < 500 {
			s.logger.Warn("webhook returned client error, not retrying",
				zap.Int("status_code", result.StatusCode),
				zap.String("request_id", payload.RequestID),
			)
			return fmt.Errorf("webhook returned status %d: %w", result.StatusCode, result.Error)
		}
	}

	return fmt.Errorf("webhook delivery failed after %d attempts: %w", maxRetries, lastErr)
}

// DeliverAsync sends a webhook payload asynchronously
func (s *DeliveryService) DeliverAsync(webhookCfg Config, payload Payload) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		if err := s.Deliver(ctx, webhookCfg, payload); err != nil {
			s.logger.Error("async webhook delivery failed",
				zap.Error(err),
				zap.String("request_id", payload.RequestID),
				zap.String("webhook_url", webhookCfg.URL),
			)
		}
	}()
}

// deliverOnce makes a single webhook delivery attempt
func (s *DeliveryService) deliverOnce(ctx context.Context, webhookCfg Config, payload Payload) DeliveryResult {
	body, err := json.Marshal(payload)
	if err != nil {
		return DeliveryResult{
			Success: false,
			Error:   fmt.Errorf("marshaling payload: %w", err),
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookCfg.URL, bytes.NewReader(body))
	if err != nil {
		return DeliveryResult{
			Success: false,
			Error:   fmt.Errorf("creating request: %w", err),
		}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Forge-Platform/1.0")

	// Add HMAC signature if secret is configured
	if webhookCfg.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		signature := s.computeSignature(timestamp, body, webhookCfg.Secret)
		req.Header.Set("X-Forge-Signature", "sha256="+signature)
		req.Header.Set("X-Forge-Timestamp", timestamp)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return DeliveryResult{
			Success: false,
			Error:   fmt.Errorf("sending request: %w", err),
		}
	}
	defer resp.Body.Close()

	// Read response body for logging
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.logger.Debug("webhook delivered successfully",
			zap.String("request_id", payload.RequestID),
			zap.Int("status_code", resp.StatusCode),
		)
		return DeliveryResult{
			Success:    true,
			StatusCode: resp.StatusCode,
		}
	}

	s.logger.Warn("webhook delivery failed",
		zap.String("request_id", payload.RequestID),
		zap.Int("status_code", resp.StatusCode),
		zap.String("response_body", string(respBody)),
	)

	return DeliveryResult{
		Success:    false,
		StatusCode: resp.StatusCode,
		Error:      fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody)),
	}
}

// computeSignature computes the HMAC-SHA256 signature for a webhook payload
func (s *DeliveryService) computeSignature(timestamp string, body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// isCircuitOpen checks if the circuit breaker is open for a URL
func (s *DeliveryService) isCircuitOpen(url string) bool {
	s.circuitMu.RLock()
	defer s.circuitMu.RUnlock()

	state, ok := s.circuitStates[url]
	if !ok {
		return false
	}

	if state.openUntil.After(time.Now()) {
		return true
	}

	return false
}

// recordFailure records a delivery failure for circuit breaker logic
func (s *DeliveryService) recordFailure(url string, _ error) {
	s.circuitMu.Lock()
	defer s.circuitMu.Unlock()

	state, ok := s.circuitStates[url]
	if !ok {
		state = &circuitState{}
		s.circuitStates[url] = state
	}

	state.failures++
	state.lastFailed = time.Now()

	if state.failures >= s.cfg.WebhookCircuitThreshold {
		state.openUntil = time.Now().Add(s.cfg.WebhookCircuitTimeout)
		s.logger.Warn("circuit breaker opened",
			zap.String("webhook_url", url),
			zap.Int("failures", state.failures),
			zap.Time("open_until", state.openUntil),
		)
	}
}

// recordSuccess records a successful delivery and resets circuit breaker
func (s *DeliveryService) recordSuccess(url string) {
	s.circuitMu.Lock()
	defer s.circuitMu.Unlock()

	if state, ok := s.circuitStates[url]; ok {
		if state.failures > 0 {
			s.logger.Info("circuit breaker reset after success",
				zap.String("webhook_url", url),
			)
		}
		state.failures = 0
		state.openUntil = time.Time{}
	}
}

// CreateDeliveryRecord creates a webhook delivery record in the database
func (s *DeliveryService) CreateDeliveryRecord(ctx context.Context, requestID, agentID string, webhookCfg Config) error {
	var secretHash sql.NullString
	if webhookCfg.Secret != "" {
		hash := sha256.Sum256([]byte(webhookCfg.Secret))
		secretHash = sql.NullString{String: hex.EncodeToString(hash[:]), Valid: true}
	}

	_, err := s.queries.CreateWebhookDelivery(ctx, &sqlc.CreateWebhookDeliveryParams{
		RequestID:         requestID,
		AgentID:           agentID,
		WebhookUrl:        webhookCfg.URL,
		WebhookSecretHash: secretHash,
	})
	if err != nil {
		return fmt.Errorf("creating webhook delivery record: %w", err)
	}

	return nil
}

// UpdateDeliverySeq updates the sequence number for a delivery
func (s *DeliveryService) UpdateDeliverySeq(ctx context.Context, requestID string, seq int64, eventType EventType) error {
	return s.queries.UpdateDeliverySeq(ctx, &sqlc.UpdateDeliverySeqParams{
		RequestID:     requestID,
		Seq:           seq,
		LastEventType: sql.NullString{String: string(eventType), Valid: true},
	})
}

// MarkDeliveryCompleted marks a delivery as completed
func (s *DeliveryService) MarkDeliveryCompleted(ctx context.Context, requestID string) error {
	return s.queries.MarkDeliveryCompleted(ctx, requestID)
}

// MarkDeliveryFailed marks a delivery as failed
func (s *DeliveryService) MarkDeliveryFailed(ctx context.Context, requestID string) error {
	return s.queries.MarkDeliveryFailed(ctx, requestID)
}

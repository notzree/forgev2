package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds all application configuration
type Config struct {
	Port               int           `env:"PORT" envDefault:"8080"`
	DebugMode          bool          `env:"DEBUG" envDefault:"false"`
	CORSAllowedOrigins []string      `env:"CORS_ORIGINS" envSeparator:","`
	ShutdownTimeout    time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	ReadTimeout        time.Duration `env:"READ_TIMEOUT" envDefault:"10s"`
	WriteTimeout       time.Duration `env:"WRITE_TIMEOUT" envDefault:"10s"`

	// Database configuration
	DatabaseURL string `env:"DATABASE_URL" envDefault:"postgres://forge:forge@localhost:5432/forge_dev?sslmode=disable"`

	// Kubernetes configuration
	KubeConfigPath string `env:"KUBE_CONFIG_PATH"`
	AgentNamespace string `env:"AGENT_NAMESPACE" envDefault:"default"`
	// NodeHost is the host/IP for accessing NodePort services (e.g., "localhost")
	// Set this when running platform locally outside the cluster
	// Leave empty when running platform inside the cluster (uses pod IPs directly)
	NodeHost string `env:"NODE_HOST"`

	// Webhook configuration
	WebhookTimeout          time.Duration `env:"WEBHOOK_TIMEOUT" envDefault:"10s"`
	WebhookMaxRetries       int           `env:"WEBHOOK_MAX_RETRIES" envDefault:"5"`
	WebhookCircuitThreshold int           `env:"WEBHOOK_CIRCUIT_THRESHOLD" envDefault:"5"`
	WebhookCircuitTimeout   time.Duration `env:"WEBHOOK_CIRCUIT_TIMEOUT" envDefault:"60s"`
}

// New creates a new Config from environment variables
func New() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

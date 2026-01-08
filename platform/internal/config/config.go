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
	TursoURL           string        `env:"TURSO_URL"`
	TursoToken         string        `env:"TURSO_AUTH_TOKEN"`
	ShutdownTimeout    time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	ReadTimeout        time.Duration `env:"READ_TIMEOUT" envDefault:"10s"`
	WriteTimeout       time.Duration `env:"WRITE_TIMEOUT" envDefault:"10s"`
}

// New creates a new Config from environment variables
func New() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

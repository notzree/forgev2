package logger

import (
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/forge/platform/internal/config"
)

// New creates a new zap logger based on configuration
func New(cfg *config.Config) (*zap.Logger, error) {
	if cfg.DebugMode {
		return zap.NewDevelopment()
	}
	return zap.NewProduction()
}

// Module provides the logger to the fx container
var Module = fx.Module("logger",
	fx.Provide(New),
)

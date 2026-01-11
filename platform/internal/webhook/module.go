package webhook

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/forge/platform/internal/config"
)

// Module provides webhook components to the fx container
var Module = fx.Module("webhook",
	fx.Provide(newDeliveryService),
)

// newDeliveryService creates a new DeliveryService using configuration from the fx container
func newDeliveryService(pool *pgxpool.Pool, cfg *config.Config, logger *zap.Logger) *DeliveryService {
	return NewDeliveryService(pool, cfg, logger)
}

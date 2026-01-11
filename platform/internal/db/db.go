package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/forge/platform/internal/config"
)

// Module provides database components to the fx container
var Module = fx.Module("db",
	fx.Provide(NewPool),
)

// NewPool creates a new PostgreSQL connection pool
func NewPool(lc fx.Lifecycle, cfg *config.Config, logger *zap.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing database URL: %w", err)
	}

	// Configure pool settings
	poolCfg.MaxConns = 25
	poolCfg.MinConns = 5

	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	logger.Info("connected to database",
		zap.Int32("max_conns", poolCfg.MaxConns),
		zap.Int32("min_conns", poolCfg.MinConns),
	)

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			logger.Info("closing database connection pool")
			pool.Close()
			return nil
		},
	})

	return pool, nil
}

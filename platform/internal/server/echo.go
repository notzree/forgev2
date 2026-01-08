package server

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/forge/platform/internal/config"
)

// NewEcho creates a new Echo instance with lifecycle management
func NewEcho(lc fx.Lifecycle, cfg *config.Config, logger *zap.Logger) *echo.Echo {
	e := echo.New()
	e.HideBanner = false
	e.HidePort = true
	e.Server.ReadTimeout = cfg.ReadTimeout
	e.Server.WriteTimeout = cfg.WriteTimeout

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			addr := fmt.Sprintf(":%d", cfg.Port)
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("failed to listen on %s: %w", addr, err)
			}

			logger.Info("Starting HTTP server", zap.String("addr", addr))
			go func() {
				if err := e.Server.Serve(ln); err != nil && err != http.ErrServerClosed {
					logger.Error("Server error", zap.Error(err))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("Stopping HTTP server")
			return e.Shutdown(ctx)
		},
	})

	return e
}

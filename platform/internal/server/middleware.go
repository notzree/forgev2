package server

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"

	"github.com/forge/platform/internal/config"
	"github.com/forge/platform/internal/errors"
)

// SetupMiddleware configures all middleware for the Echo instance
func SetupMiddleware(e *echo.Echo, cfg *config.Config, logger *zap.Logger) {
	// Recover from panics
	e.Use(middleware.Recover())

	// Request ID for tracing
	e.Use(middleware.RequestID())

	// CORS
	if len(cfg.CORSAllowedOrigins) > 0 {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins:     cfg.CORSAllowedOrigins,
			AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
			AllowHeaders:     []string{echo.HeaderContentType, echo.HeaderAuthorization},
			AllowCredentials: true,
			MaxAge:           86400,
		}))
	} else {
		e.Use(middleware.CORS())
	}

	// Request logging (conditional)
	if cfg.DebugMode {
		e.Use(requestLoggerMiddleware(logger))
	}

	// Custom error handler
	e.HTTPErrorHandler = errors.HTTPErrorHandler(logger)
}

func requestLoggerMiddleware(logger *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)

			logger.Info("request",
				zap.String("method", c.Request().Method),
				zap.String("path", c.Path()),
				zap.Int("status", c.Response().Status),
				zap.Duration("latency", time.Since(start)),
				zap.String("request_id", c.Response().Header().Get(echo.HeaderXRequestID)),
			)
			return err
		}
	}
}

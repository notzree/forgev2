package main

import (
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"

	"github.com/forge/platform/internal/agent"
	agenthandler "github.com/forge/platform/internal/agent/handler"
	"github.com/forge/platform/internal/agent/processor"
	"github.com/forge/platform/internal/config"
	"github.com/forge/platform/internal/handler"
	"github.com/forge/platform/internal/logger"
	"github.com/forge/platform/internal/server"
)

func main() {
	fx.New(
		// Provide config
		fx.Provide(config.New),

		// Core modules
		logger.Module,
		agent.Module,
		processor.Module,
		agenthandler.Module,
		server.Module,
		handler.Module,

		// Configure fx logging
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log}
		}),
	).Run()
}

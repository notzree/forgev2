package handler

import (
	"go.uber.org/fx"

	"github.com/forge/platform/internal/handler"
)

// Module provides the agent handler to the fx container
var Module = fx.Module("agent.handler",
	fx.Provide(handler.AsHandler(NewHandler)),
)

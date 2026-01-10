package handler

import (
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// HandlerRegistrar is the interface for registering routes with Echo
type HandlerRegistrar interface {
	Register(e *echo.Echo)
}

// AsHandler annotates the handler constructor for the handlers group
func AsHandler(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(HandlerRegistrar)),
		fx.ResultTags(`group:"handlers"`),
	)
}

// Module provides the agent handler to the fx container
var Module = fx.Module("agent.handler",
	fx.Provide(AsHandler(NewHandler)),
)

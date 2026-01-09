package handler

import (
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// AsHandler annotates a handler constructor to be part of the handlers group
func AsHandler(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(Handler)),
		fx.ResultTags(`group:"handlers"`),
	)
}

// RegisterHandlersParams uses fx.In for parameter injection
type RegisterHandlersParams struct {
	fx.In
	Echo     *echo.Echo
	Handlers []Handler `group:"handlers"`
}

// RegisterAll registers all grouped handlers with Echo
func RegisterAll(p RegisterHandlersParams) {
	for _, h := range p.Handlers {
		h.Register(p.Echo)
	}
}

// Module provides all handlers to the fx container
var Module = fx.Module("handler",
	fx.Provide(
		AsHandler(NewHealthHandler),
		AsHandler(NewAgentHandler),
	),
	fx.Invoke(RegisterAll),
)

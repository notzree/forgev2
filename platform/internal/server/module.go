package server

import (
	"go.uber.org/fx"
)

// Module provides the server components to the fx container
var Module = fx.Module("server",
	fx.Provide(NewEcho),
	fx.Invoke(SetupMiddleware),
)

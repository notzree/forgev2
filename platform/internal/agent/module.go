package agent

import "go.uber.org/fx"

// Module provides agent components to the fx container
var Module = fx.Module("agent",
	fx.Provide(
		NewRegistry,
	),
)

package processor

import "go.uber.org/fx"

// Module provides the processor to the fx container
var Module = fx.Module("agent.processor",
	fx.Provide(NewProcessor),
)

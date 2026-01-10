package agent

import "go.uber.org/fx"

// Module provides agent components to the fx container
// TODO: Registry was deleted, this module needs to be updated (see TASKS.md Task 6)
var Module = fx.Module("agent",
	fx.Provide(
	// NewRegistry - removed, see TASKS.md Task 6
	),
)

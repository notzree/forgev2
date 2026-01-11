package k8s

import (
	"go.uber.org/fx"

	"github.com/forge/platform/internal/config"
)

// Module provides Kubernetes components to the fx container
var Module = fx.Module("k8s",
	fx.Provide(NewContainerConfig),
	fx.Provide(newManager),
)

// newManager creates a new Manager using configuration from the fx container
func newManager(cfg *config.Config, containerCfg *ContainerConfig) (*Manager, error) {
	return NewManager(ManagerOpts{
		KubeConfigPath: cfg.KubeConfigPath,
		ContainerCfg:   *containerCfg,
		AgentNamespace: cfg.AgentNamespace,
		NodeHost:       cfg.NodeHost,
	})
}

package k8s

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// ContainerConfig holds container registry configuration
type ContainerConfig struct {
	// Registry is the container registry host (e.g., "ghcr.io", "docker.io")
	Registry string `env:"CONTAINER_REGISTRY" envDefault:"ghcr.io"`

	// Namespace is the registry namespace/org (e.g., "notzree", "myorg")
	// Leave empty for registries that don't use namespaces (like local k3d registry)
	Namespace string `env:"CONTAINER_NAMESPACE"`

	// AgentImageName is the name of the agent image
	AgentImageName string `env:"AGENT_IMAGE_NAME" envDefault:"forge-agent"`

	// AgentImageTag is the tag to use (e.g., "latest", "v1.0.0", git sha)
	AgentImageTag string `env:"AGENT_IMAGE_TAG" envDefault:"latest"`

	// ImagePullSecret is the name of the K8s secret for private registry auth
	// Leave empty for public images
	ImagePullSecret string `env:"IMAGE_PULL_SECRET" envDefault:""`
}

// NewContainerConfig creates a new ContainerConfig from environment variables
func NewContainerConfig() (*ContainerConfig, error) {
	cfg := &ContainerConfig{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parsing container config: %w", err)
	}
	return cfg, nil
}

// AgentImage returns the full image reference for the agent
// e.g., "ghcr.io/notzree/forge-agent:latest" or "registry:5111/forge-agent:latest"
func (c *ContainerConfig) AgentImage() string {
	if c.Namespace == "" {
		return fmt.Sprintf("%s/%s:%s", c.Registry, c.AgentImageName, c.AgentImageTag)
	}
	return fmt.Sprintf("%s/%s/%s:%s", c.Registry, c.Namespace, c.AgentImageName, c.AgentImageTag)
}

// AgentImageWithTag returns the agent image with a specific tag override
func (c *ContainerConfig) AgentImageWithTag(tag string) string {
	if c.Namespace == "" {
		return fmt.Sprintf("%s/%s:%s", c.Registry, c.AgentImageName, tag)
	}
	return fmt.Sprintf("%s/%s/%s:%s", c.Registry, c.Namespace, c.AgentImageName, tag)
}

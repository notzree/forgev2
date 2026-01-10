package agent

import (
	"net/http"

	"github.com/forge/platform/gen/agent/v1/agentv1connect"
)

// NewClient creates a new AgentService client for the given base URL.
// The baseURL should be in the format "http://<ip>:8080".
// Clients are stateless and safe to create per-request.
//
// Uses the Connect protocol (HTTP/1.1 compatible, human-readable).
func NewClient(baseURL string) agentv1connect.AgentServiceClient {
	return agentv1connect.NewAgentServiceClient(
		http.DefaultClient,
		baseURL,
	)
}

// NewClientWithHTTPClient creates a new AgentService client with a custom HTTP client.
// Useful for testing or when custom transport configuration is needed
// (e.g., timeouts, TLS settings, tracing middleware).
func NewClientWithHTTPClient(baseURL string, httpClient *http.Client) agentv1connect.AgentServiceClient {
	return agentv1connect.NewAgentServiceClient(
		httpClient,
		baseURL,
	)
}

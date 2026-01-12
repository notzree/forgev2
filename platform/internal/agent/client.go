package agent

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/forge/platform/gen/agent/v1/agentv1connect"
	"golang.org/x/net/http2"
)

// http2Client is an HTTP client configured for HTTP/2 cleartext (h2c).
// Required for bidirectional streaming with Connect-RPC.
//
// Note: No Timeout is set because http.Client.Timeout applies to the entire
// request/response cycle including reading the body. For long-running
// bidirectional streams, this would cause premature disconnects.
// Use context cancellation for timeout control instead.
var http2Client = &http.Client{
	Transport: &http2.Transport{
		// Allow h2c (HTTP/2 without TLS)
		AllowHTTP: true,
		// Use a custom DialTLSContext that returns a plain connection
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	},
	Timeout: time.Duration(0),
}

// NewClient creates a new AgentService client for the given base URL.
// The baseURL should be in the format "http://<ip>:8080".
// Clients are stateless and safe to create per-request.
//
// Uses HTTP/2 cleartext (h2c) for bidirectional streaming support.
func NewClient(baseURL string) agentv1connect.AgentServiceClient {
	return agentv1connect.NewAgentServiceClient(
		http2Client,
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

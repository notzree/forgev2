package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	agentv1 "github.com/forge/platform/gen/agent/v1"
	"github.com/forge/platform/gen/agent/v1/agentv1connect"
)

func TestNewClient_ReturnsValidClient(t *testing.T) {
	client := NewClient("http://10.0.0.1:8080")
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify it implements the interface
	var _ agentv1connect.AgentServiceClient = client
}

func TestNewClient_WithDifferentURLFormats(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
	}{
		{"with port", "http://10.0.0.1:8080"},
		{"localhost", "http://localhost:8080"},
		{"with trailing slash", "http://10.0.0.1:8080/"},
		{"dns name", "http://agent-pod.default.svc.cluster.local:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.baseURL)
			if client == nil {
				t.Errorf("NewClient(%q) returned nil", tt.baseURL)
			}
		})
	}
}

func TestNewClientWithHTTPClient_UsesProvidedClient(t *testing.T) {
	customClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	client := NewClientWithHTTPClient("http://10.0.0.1:8080", customClient)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	var _ agentv1connect.AgentServiceClient = client
}

func TestNewClient_CanCallGetStatus(t *testing.T) {
	// Create a mock server implementing the AgentService
	mux := http.NewServeMux()
	path, handler := agentv1connect.NewAgentServiceHandler(&mockAgentService{})
	mux.Handle(path, handler)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Create client and make a call
	client := NewClient(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetStatus(ctx, connect.NewRequest(&agentv1.GetStatusRequest{}))
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if resp.Msg.AgentId != "test-agent" {
		t.Errorf("expected agent_id 'test-agent', got %q", resp.Msg.AgentId)
	}

	if resp.Msg.State != agentv1.AgentState_AGENT_STATE_IDLE {
		t.Errorf("expected state AGENT_STATE_IDLE, got %v", resp.Msg.State)
	}
}

func TestNewClient_CanCallShutdown(t *testing.T) {
	mux := http.NewServeMux()
	path, handler := agentv1connect.NewAgentServiceHandler(&mockAgentService{})
	mux.Handle(path, handler)

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Shutdown(ctx, connect.NewRequest(&agentv1.ShutdownRequest{
		Graceful: true,
	}))
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if !resp.Msg.Success {
		t.Error("expected shutdown success to be true")
	}
}

// mockAgentService implements agentv1connect.AgentServiceHandler for testing
type mockAgentService struct {
	agentv1connect.UnimplementedAgentServiceHandler
}

func (m *mockAgentService) GetStatus(
	ctx context.Context,
	req *connect.Request[agentv1.GetStatusRequest],
) (*connect.Response[agentv1.GetStatusResponse], error) {
	return connect.NewResponse(&agentv1.GetStatusResponse{
		AgentId:        "test-agent",
		SessionId:      "test-session",
		State:          agentv1.AgentState_AGENT_STATE_IDLE,
		CurrentModel:   "claude-sonnet-4-20250514",
		PermissionMode: "acceptEdits",
		UptimeMs:       1000,
	}), nil
}

func (m *mockAgentService) Shutdown(
	ctx context.Context,
	req *connect.Request[agentv1.ShutdownRequest],
) (*connect.Response[agentv1.ShutdownResponse], error) {
	return connect.NewResponse(&agentv1.ShutdownResponse{
		Success: true,
	}), nil
}

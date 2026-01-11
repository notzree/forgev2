package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/forge/platform/internal/agent/processor"
	"github.com/forge/platform/internal/errors"
	"github.com/forge/platform/internal/k8s"
)

const testNamespace = "test-ns"

// createTestProcessor creates a processor with a fake K8s clientset for testing
func createTestProcessor(t *testing.T, objects ...runtime.Object) *processor.Processor {
	t.Helper()
	clientset := fake.NewSimpleClientset(objects...)
	mgr := k8s.NewManagerWithClientset(clientset, testNamespace, "test-image:latest")
	return processor.NewProcessor(mgr)
}

// createReadyPod creates a pod that is in ready state
func createReadyPod(userID, agentID string) *corev1.Pod {
	podID := k8s.NewPodID(userID, agentID)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              podID.Name(),
			Namespace:         testNamespace,
			CreationTimestamp: metav1.NewTime(time.Now()),
			Labels: map[string]string{
				"user-id":  userID,
				"agent-id": agentID,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.1",
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}
}

// createPendingPod creates a pod that is in pending state
func createPendingPod(userID, agentID string) *corev1.Pod {
	podID := k8s.NewPodID(userID, agentID)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              podID.Name(),
			Namespace:         testNamespace,
			CreationTimestamp: metav1.NewTime(time.Now()),
			Labels: map[string]string{
				"user-id":  userID,
				"agent-id": agentID,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
}

// setupTestHandler creates an Echo instance with the handler registered
func setupTestHandler(t *testing.T, proc *processor.Processor) *echo.Echo {
	t.Helper()
	e := echo.New()
	logger := zap.NewNop()
	e.HTTPErrorHandler = errors.HTTPErrorHandler(logger)
	h := NewHandler(proc)
	h.Register(e)
	return e
}

// --- List Handler Tests ---

func TestList_Success(t *testing.T) {
	pod1 := createReadyPod("user1", "agent1")
	pod2 := createReadyPod("user1", "agent2")
	proc := createTestProcessor(t, pod1, pod2)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents?user_id=user1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp ListAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("expected 2 agents, got %d", resp.Total)
	}
	if len(resp.Agents) != 2 {
		t.Errorf("expected 2 agents in list, got %d", len(resp.Agents))
	}
}

func TestList_Empty(t *testing.T) {
	proc := createTestProcessor(t)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents?user_id=user1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp ListAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Total != 0 {
		t.Errorf("expected 0 agents, got %d", resp.Total)
	}
}

func TestList_MissingUserID(t *testing.T) {
	proc := createTestProcessor(t)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestList_FiltersbyUser(t *testing.T) {
	pod1 := createReadyPod("user1", "agent1")
	pod2 := createReadyPod("user2", "agent2")
	proc := createTestProcessor(t, pod1, pod2)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents?user_id=user1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp ListAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("expected 1 agent for user1, got %d", resp.Total)
	}
	if len(resp.Agents) == 1 && resp.Agents[0].UserID != "user1" {
		t.Errorf("expected agent for user1, got %s", resp.Agents[0].UserID)
	}
}

// --- Get Handler Tests ---

func TestGet_Success(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	proc := createTestProcessor(t, pod)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent1?user_id=user1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp AgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.UserID != "user1" {
		t.Errorf("expected user_id user1, got %s", resp.UserID)
	}
	if resp.AgentID != "agent1" {
		t.Errorf("expected agent_id agent1, got %s", resp.AgentID)
	}
	if resp.PodIP != "10.0.0.1" {
		t.Errorf("expected pod_ip 10.0.0.1, got %s", resp.PodIP)
	}
	if resp.Phase != corev1.PodRunning {
		t.Errorf("expected phase Running, got %s", resp.Phase)
	}
	if !resp.Ready {
		t.Error("expected ready to be true")
	}
}

func TestGet_NotFound(t *testing.T) {
	proc := createTestProcessor(t)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/nonexistent?user_id=user1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestGet_MissingUserID(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	proc := createTestProcessor(t, pod)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestGet_PendingPod(t *testing.T) {
	pod := createPendingPod("user1", "agent1")
	proc := createTestProcessor(t, pod)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent1?user_id=user1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp AgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Phase != corev1.PodPending {
		t.Errorf("expected phase Pending, got %s", resp.Phase)
	}
	if resp.Ready {
		t.Error("expected ready to be false for pending pod")
	}
}

// --- Delete Handler Tests ---

func TestDelete_Success(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	proc := createTestProcessor(t, pod)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/agent1?user_id=user1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestDelete_NotFound(t *testing.T) {
	proc := createTestProcessor(t)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/nonexistent?user_id=user1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestDelete_MissingUserID(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	proc := createTestProcessor(t, pod)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/agent1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestDelete_GracefulParam(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	proc := createTestProcessor(t, pod)
	e := setupTestHandler(t, proc)

	// With graceful=true, should still succeed (graceful shutdown errors are ignored)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/agent1?user_id=user1&graceful=true", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}

// --- Create Handler Tests ---

func TestCreate_MissingOwnerID(t *testing.T) {
	proc := createTestProcessor(t)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestCreate_InvalidJSON(t *testing.T) {
	proc := createTestProcessor(t)
	e := setupTestHandler(t, proc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(`invalid json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// --- Helper Function Tests ---

func TestIsPodReady_Running(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	if !isPodReady(pod) {
		t.Error("expected running pod with ready containers to be ready")
	}
}

func TestIsPodReady_Pending(t *testing.T) {
	pod := createPendingPod("user1", "agent1")
	if isPodReady(pod) {
		t.Error("expected pending pod to not be ready")
	}
}

func TestIsPodReady_ContainerNotReady(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	pod.Status.ContainerStatuses[0].Ready = false
	if isPodReady(pod) {
		t.Error("expected pod with unready container to not be ready")
	}
}

func TestPodToAgentResponse(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	resp := podToAgentResponse(pod)

	if resp.UserID != "user1" {
		t.Errorf("expected user_id user1, got %s", resp.UserID)
	}
	if resp.AgentID != "agent1" {
		t.Errorf("expected agent_id agent1, got %s", resp.AgentID)
	}
	if resp.PodName != "user1-agent1" {
		t.Errorf("expected pod_name user1-agent1, got %s", resp.PodName)
	}
	if resp.PodIP != "10.0.0.1" {
		t.Errorf("expected pod_ip 10.0.0.1, got %s", resp.PodIP)
	}
	if resp.Phase != corev1.PodRunning {
		t.Errorf("expected phase Running, got %s", resp.Phase)
	}
	if !resp.Ready {
		t.Error("expected ready to be true")
	}
	if resp.CreatedAt == "" {
		t.Error("expected created_at to be set")
	}
}

func TestPodToAgentResponse_PendingPod(t *testing.T) {
	pod := createPendingPod("user1", "agent1")
	resp := podToAgentResponse(pod)

	if resp.Phase != corev1.PodPending {
		t.Errorf("expected phase Pending, got %s", resp.Phase)
	}
	if resp.Ready {
		t.Error("expected ready to be false")
	}
	if resp.PodIP != "" {
		t.Errorf("expected empty pod_ip for pending pod, got %s", resp.PodIP)
	}
}

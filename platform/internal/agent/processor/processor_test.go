package processor

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	agentv1 "github.com/forge/platform/gen/agent/v1"
	"github.com/forge/platform/gen/agent/v1/agentv1connect"
	"github.com/forge/platform/internal/k8s"
)

const testNamespace = "test-ns"

// createTestK8sManager creates a k8s.Manager with a fake clientset for testing
func createTestK8sManager(t *testing.T, objects ...runtime.Object) *k8s.Manager {
	t.Helper()
	clientset := fake.NewSimpleClientset(objects...)
	return k8s.NewManagerWithClientset(clientset, testNamespace, "test-image:latest", "")
}

// createTestProcessor creates a processor with a fake K8s manager for testing
func createTestProcessor(t *testing.T, mgr *k8s.Manager) *Processor {
	t.Helper()
	return NewProcessor(mgr, nil, zap.NewNop())
}

// createReadyPod creates a pod that is in ready state
func createReadyPod(userID, agentID string) *corev1.Pod {
	podID := k8s.NewPodID(userID, agentID)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podID.Name(),
			Namespace: testNamespace,
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
			Name:      podID.Name(),
			Namespace: testNamespace,
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

// mockAgentService implements agentv1connect.AgentServiceHandler for testing
type mockAgentService struct {
	agentv1connect.UnimplementedAgentServiceHandler
	getStatusCalled  bool
	shutdownCalled   bool
	shutdownGraceful bool
}

func (m *mockAgentService) GetStatus(
	ctx context.Context,
	req *connect.Request[agentv1.GetStatusRequest],
) (*connect.Response[agentv1.GetStatusResponse], error) {
	m.getStatusCalled = true
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
	m.shutdownCalled = true
	m.shutdownGraceful = req.Msg.Graceful
	return connect.NewResponse(&agentv1.ShutdownResponse{
		Success: true,
	}), nil
}

// --- ListAgents Tests ---

func TestListAgents_Empty(t *testing.T) {
	mgr := createTestK8sManager(t)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	podIDs, err := proc.ListAgents(ctx, "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(podIDs) != 0 {
		t.Errorf("expected empty list, got %d items", len(podIDs))
	}
}

func TestListAgents_SingleAgent(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	mgr := createTestK8sManager(t, pod)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	podIDs, err := proc.ListAgents(ctx, "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(podIDs) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(podIDs))
	}
	if podIDs[0].UserID != "user1" || podIDs[0].AgentID != "agent1" {
		t.Errorf("unexpected pod ID: %+v", podIDs[0])
	}
}

func TestListAgents_MultipleAgents(t *testing.T) {
	pod1 := createReadyPod("user1", "agent1")
	pod2 := createReadyPod("user1", "agent2")
	pod3 := createReadyPod("user2", "agent3") // Different user
	mgr := createTestK8sManager(t, pod1, pod2, pod3)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	podIDs, err := proc.ListAgents(ctx, "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(podIDs) != 2 {
		t.Fatalf("expected 2 agents for user1, got %d", len(podIDs))
	}

	// Verify we got the right agents
	agentIDs := make(map[string]bool)
	for _, pid := range podIDs {
		if pid.UserID != "user1" {
			t.Errorf("expected user1, got %s", pid.UserID)
		}
		agentIDs[pid.AgentID] = true
	}
	if !agentIDs["agent1"] || !agentIDs["agent2"] {
		t.Errorf("expected agent1 and agent2, got %v", agentIDs)
	}
}

// --- GetAgent Tests ---

func TestGetAgent_Found(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	mgr := createTestK8sManager(t, pod)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	result, err := proc.GetAgent(ctx, "user1", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != pod.Name {
		t.Errorf("expected pod name %s, got %s", pod.Name, result.Name)
	}
	if result.Status.PodIP != "10.0.0.1" {
		t.Errorf("expected pod IP 10.0.0.1, got %s", result.Status.PodIP)
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	mgr := createTestK8sManager(t)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	_, err := proc.GetAgent(ctx, "user1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

// --- GetStatus Tests ---

func TestGetStatus_Success(t *testing.T) {
	// Note: This test won't fully work because the httptest server uses a random port
	// and GetPodAddress always uses port 8080. This is a limitation we document.
	// In a real integration test, we'd need to configure the port differently
	// or use a more sophisticated mock setup.
	t.Skip("Skipping: requires matching mock server port to k8s.DefaultAgentPort")
}

func TestGetStatus_AgentNotFound(t *testing.T) {
	mgr := createTestK8sManager(t)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	_, err := proc.GetStatus(ctx, "user1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestGetStatus_AgentNotReady(t *testing.T) {
	// Pod exists but has no IP
	pod := createPendingPod("user1", "agent1")
	mgr := createTestK8sManager(t, pod)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	_, err := proc.GetStatus(ctx, "user1", "agent1")
	if err == nil {
		t.Fatal("expected error for agent without IP")
	}
}

// --- DeleteAgent Tests ---

func TestDeleteAgent_ForceDelete(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	mgr := createTestK8sManager(t, pod)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	err := proc.DeleteAgent(ctx, "user1", "agent1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify pod is deleted
	_, err = proc.GetAgent(ctx, "user1", "agent1")
	if err == nil {
		t.Error("expected agent to be deleted")
	}
}

func TestDeleteAgent_NotFound(t *testing.T) {
	mgr := createTestK8sManager(t)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	err := proc.DeleteAgent(ctx, "user1", "nonexistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestDeleteAgent_GracefulWithUnreachableAgent(t *testing.T) {
	// Pod exists but agent is unreachable (no mock server)
	// Graceful should still succeed (delete happens regardless)
	pod := createReadyPod("user1", "agent1")
	mgr := createTestK8sManager(t, pod)
	proc := createTestProcessor(t, mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This should succeed because graceful shutdown errors are ignored
	err := proc.DeleteAgent(ctx, "user1", "agent1", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify pod is deleted
	_, err = proc.GetAgent(ctx, "user1", "agent1")
	if err == nil {
		t.Error("expected agent to be deleted")
	}
}

// --- CreateAgent Tests ---

func TestCreateAgent_Success(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	// Set up watch reactor to simulate pod becoming ready
	fakeWatcher := watch.NewFake()
	clientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(fakeWatcher, nil))

	mgr := k8s.NewManagerWithClientset(clientset, testNamespace, "test-image:latest", "")
	proc := createTestProcessor(t, mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run CreateAgent in a goroutine
	type result struct {
		podID *k8s.PodID
		err   error
	}
	resultCh := make(chan result, 1)

	go func() {
		podID, err := proc.CreateAgent(ctx, "user1")
		resultCh <- result{podID, err}
	}()

	// Give time for pod creation
	time.Sleep(100 * time.Millisecond)

	// Get the created pod name
	pods, err := clientset.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}
	if len(pods.Items) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods.Items))
	}

	createdPod := pods.Items[0]

	// Simulate pod becoming ready
	readyPod := createdPod.DeepCopy()
	readyPod.Status.Phase = corev1.PodRunning
	readyPod.Status.PodIP = "10.0.0.5"
	readyPod.Status.ContainerStatuses = []corev1.ContainerStatus{{Ready: true}}
	fakeWatcher.Modify(readyPod)

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("unexpected error: %v", res.err)
		}
		if res.podID == nil {
			t.Fatal("expected non-nil podID")
		}
		if res.podID.UserID != "user1" {
			t.Errorf("expected userID user1, got %s", res.podID.UserID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for CreateAgent to return")
	}
}

func TestCreateAgent_ContextCancelled(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	// Set up watch reactor but don't send ready event
	fakeWatcher := watch.NewFake()
	clientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(fakeWatcher, nil))

	mgr := k8s.NewManagerWithClientset(clientset, testNamespace, "test-image:latest", "")
	proc := createTestProcessor(t, mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := proc.CreateAgent(ctx, "user1")
	if err == nil {
		t.Fatal("expected error when context times out")
	}

	// Verify pod was cleaned up (best effort, may or may not be deleted depending on timing)
	time.Sleep(100 * time.Millisecond)
	pods, _ := clientset.CoreV1().Pods(testNamespace).List(context.Background(), metav1.ListOptions{})
	t.Logf("Pods remaining after failed create: %d (cleanup may be async)", len(pods.Items))
}

// --- ConnectToAgent Tests ---

func TestConnectToAgent_AgentNotFound(t *testing.T) {
	mgr := createTestK8sManager(t)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	_, err := proc.ConnectToAgent(ctx, "user1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestConnectToAgent_AgentNotReady(t *testing.T) {
	pod := createPendingPod("user1", "agent1")
	mgr := createTestK8sManager(t, pod)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	_, err := proc.ConnectToAgent(ctx, "user1", "agent1")
	if err == nil {
		t.Fatal("expected error for agent without IP")
	}
}

func TestConnectToAgent_ReturnsStream(t *testing.T) {
	pod := createReadyPod("user1", "agent1")
	mgr := createTestK8sManager(t, pod)
	proc := createTestProcessor(t, mgr)

	ctx := context.Background()
	stream, err := proc.ConnectToAgent(ctx, "user1", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stream == nil {
		t.Fatal("expected non-nil stream")
	}
	// Note: The stream is returned but not connected to a real server
	// Actual streaming tests would be integration tests
}

// --- Helper Tests ---

func TestGenerateAgentID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateAgentID()
		if ids[id] {
			t.Errorf("duplicate agent ID generated: %s", id)
		}
		ids[id] = true
		time.Sleep(time.Nanosecond) // Ensure different timestamps
	}
}

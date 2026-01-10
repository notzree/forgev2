package k8s

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestIsPodReady_AllConditionsMet(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.1",
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}

	if !isPodReady(pod) {
		t.Error("expected pod to be ready when all conditions are met")
	}
}

func TestIsPodReady_NotRunning(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			PodIP: "10.0.0.1",
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}

	if isPodReady(pod) {
		t.Error("expected pod to not be ready when phase is not Running")
	}
}

func TestIsPodReady_NoIP(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "",
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}

	if isPodReady(pod) {
		t.Error("expected pod to not be ready when no IP is assigned")
	}
}

func TestIsPodReady_ContainerNotReady(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.1",
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: false},
			},
		},
	}

	if isPodReady(pod) {
		t.Error("expected pod to not be ready when container is not ready")
	}
}

func TestIsPodReady_MultipleContainers_OneNotReady(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.1",
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
				{Ready: false},
				{Ready: true},
			},
		},
	}

	if isPodReady(pod) {
		t.Error("expected pod to not be ready when one container is not ready")
	}
}

func TestIsPodReady_MultipleContainers_AllReady(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.1",
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
				{Ready: true},
			},
		},
	}

	if !isPodReady(pod) {
		t.Error("expected pod to be ready when all containers are ready")
	}
}

func TestIsPodReady_NoContainers(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			PodIP:             "10.0.0.1",
			ContainerStatuses: []corev1.ContainerStatus{},
		},
	}

	// Pod with no containers but running and has IP should be considered ready
	if !isPodReady(pod) {
		t.Error("expected pod to be ready when running with IP and no containers")
	}
}

func createTestManager(t *testing.T, namespace string, objects ...runtime.Object) *Manager {
	t.Helper()
	clientset := fake.NewSimpleClientset(objects...)
	return &Manager{
		clientset:      clientset,
		agentNamespace: namespace,
		agentImage:     "test-image:latest",
	}
}

func TestWaitForPodReady_AlreadyReady(t *testing.T) {
	namespace := "test-ns"
	podID := PodID{UserID: "user1", AgentID: "agent1"}

	readyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podID.Name(),
			Namespace: namespace,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.1",
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}

	mgr := createTestManager(t, namespace, readyPod)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pod, err := mgr.WaitForPodReady(ctx, podID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pod.Status.PodIP != "10.0.0.1" {
		t.Errorf("expected pod IP 10.0.0.1, got %s", pod.Status.PodIP)
	}
}

func TestWaitForPodReady_BecomesReady(t *testing.T) {
	namespace := "test-ns"
	podID := PodID{UserID: "user1", AgentID: "agent1"}

	pendingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podID.Name(),
			Namespace: namespace,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	clientset := fake.NewSimpleClientset(pendingPod)
	mgr := &Manager{
		clientset:      clientset,
		agentNamespace: namespace,
		agentImage:     "test-image:latest",
	}

	// Create a fake watcher
	fakeWatcher := watch.NewFake()
	clientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(fakeWatcher, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run WaitForPodReady in a goroutine
	resultCh := make(chan struct {
		pod *corev1.Pod
		err error
	}, 1)

	go func() {
		pod, err := mgr.WaitForPodReady(ctx, podID)
		resultCh <- struct {
			pod *corev1.Pod
			err error
		}{pod, err}
	}()

	// Give the goroutine time to start watching
	time.Sleep(50 * time.Millisecond)

	// Simulate pod becoming ready
	readyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podID.Name(),
			Namespace: namespace,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.2",
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}
	fakeWatcher.Modify(readyPod)

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("unexpected error: %v", result.err)
		}
		if result.pod.Status.PodIP != "10.0.0.2" {
			t.Errorf("expected pod IP 10.0.0.2, got %s", result.pod.Status.PodIP)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for WaitForPodReady to return")
	}
}

func TestWaitForPodReady_PodDeleted(t *testing.T) {
	namespace := "test-ns"
	podID := PodID{UserID: "user1", AgentID: "agent1"}

	pendingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podID.Name(),
			Namespace: namespace,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	clientset := fake.NewSimpleClientset(pendingPod)
	mgr := &Manager{
		clientset:      clientset,
		agentNamespace: namespace,
		agentImage:     "test-image:latest",
	}

	// Create a fake watcher
	fakeWatcher := watch.NewFake()
	clientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(fakeWatcher, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run WaitForPodReady in a goroutine
	resultCh := make(chan struct {
		pod *corev1.Pod
		err error
	}, 1)

	go func() {
		pod, err := mgr.WaitForPodReady(ctx, podID)
		resultCh <- struct {
			pod *corev1.Pod
			err error
		}{pod, err}
	}()

	// Give the goroutine time to start watching
	time.Sleep(50 * time.Millisecond)

	// Simulate pod deletion
	fakeWatcher.Delete(pendingPod)

	select {
	case result := <-resultCh:
		if result.err == nil {
			t.Fatal("expected error when pod is deleted")
		}
		if result.pod != nil {
			t.Error("expected nil pod when pod is deleted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for WaitForPodReady to return")
	}
}

func TestWaitForPodReady_ContextCancelled(t *testing.T) {
	namespace := "test-ns"
	podID := PodID{UserID: "user1", AgentID: "agent1"}

	pendingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podID.Name(),
			Namespace: namespace,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	clientset := fake.NewSimpleClientset(pendingPod)
	mgr := &Manager{
		clientset:      clientset,
		agentNamespace: namespace,
		agentImage:     "test-image:latest",
	}

	// Create a fake watcher
	fakeWatcher := watch.NewFake()
	clientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(fakeWatcher, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	// Run WaitForPodReady in a goroutine
	resultCh := make(chan struct {
		pod *corev1.Pod
		err error
	}, 1)

	go func() {
		pod, err := mgr.WaitForPodReady(ctx, podID)
		resultCh <- struct {
			pod *corev1.Pod
			err error
		}{pod, err}
	}()

	// Give the goroutine time to start watching
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	select {
	case result := <-resultCh:
		if result.err == nil {
			t.Fatal("expected error when context is cancelled")
		}
		if result.err != context.Canceled {
			t.Logf("got error: %v (acceptable, context was cancelled)", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for WaitForPodReady to return after context cancellation")
	}
}

func TestWaitForPodReady_PodNotFound(t *testing.T) {
	namespace := "test-ns"
	podID := PodID{UserID: "user1", AgentID: "agent1"}

	// Create manager with no pods
	mgr := createTestManager(t, namespace)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := mgr.WaitForPodReady(ctx, podID)
	if err == nil {
		t.Fatal("expected error when pod doesn't exist")
	}
}

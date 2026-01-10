package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// PodID uniquely identifies a pod by user and agent
type PodID struct {
	UserID  string
	AgentID string
}

func NewPodID(userID, agentID string) *PodID {
	return &PodID{
		UserID:  userID,
		AgentID: agentID,
	}
}

// Name returns the Kubernetes pod name for this PodID
func (p PodID) Name() string {
	return fmt.Sprintf("%s-%s", p.UserID, p.AgentID)
}

type ManagerOpts struct {
	KubeConfigPath string
	ContainerCfg   ContainerConfig
	AgentNamespace string
}

type Manager struct {
	agentNamespace string
	clientset      *kubernetes.Clientset
	agentImage     string
}

func NewManager(opts ManagerOpts) (*Manager, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", opts.KubeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build kube config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get clientset: %w", err)
	}

	return &Manager{
		clientset:      clientset,
		agentNamespace: opts.AgentNamespace,
		agentImage:     opts.ContainerCfg.AgentImageName,
	}, nil
}

func (m *Manager) CreatePod(ctx context.Context, podID PodID) error {
	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podID.Name(),
			Labels: map[string]string{
				"user-id":  podID.UserID,
				"agent-id": podID.AgentID,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "forge-agent",
					Image: m.agentImage,
					Ports: []corev1.ContainerPort{
						{ContainerPort: 8080}, //todo: richard figure out how to stop hard coding this lmfao
					},
				},
			},
		},
	}
	_, err := m.clientset.CoreV1().Pods(m.agentNamespace).Create(
		ctx,
		newPod,
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to create pod: %w", err)
	}
	return nil
}

func (m *Manager) GetPod(ctx context.Context, podID PodID) (*corev1.Pod, error) {
	pod, err := m.clientset.CoreV1().Pods(m.agentNamespace).Get(ctx, podID.Name(), metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %w", podID.Name(), err)
	}
	return pod, nil
}

func (m *Manager) ListPodsForUser(ctx context.Context, userID string) (*corev1.PodList, error) {
	pods, err := m.clientset.CoreV1().Pods(m.agentNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: UserIDLabel(userID),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list pods for %s: %w", userID, err)
	}
	return pods, nil
}

func (m *Manager) ClosePod(ctx context.Context, podID PodID) error {
	podName := podID.Name()

	err := m.clientset.CoreV1().Pods(m.agentNamespace).Delete(
		ctx,
		podName,
		metav1.DeleteOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to delete pod %s: %w", podName, err)
	}

	return nil
}

func (m *Manager) ClosePodsForUser(ctx context.Context, userID string) error {
	err := m.clientset.CoreV1().Pods(m.agentNamespace).DeleteCollection(
		ctx,
		metav1.DeleteOptions{},
		metav1.ListOptions{
			LabelSelector: UserIDLabel(userID),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to delete pods for user %s: %w", userID, err)
	}

	return nil
}

func (m *Manager) RestartPod(ctx context.Context, podID PodID) error {
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()

	events, err := m.WatchPod(watchCtx, podID)
	if err != nil {
		return fmt.Errorf("error initializing watch for pod: %w", err)
	}

	if err := m.ClosePod(ctx, podID); err != nil {
		return fmt.Errorf("error closing pod during restart: %w", err)
	}

	for event := range events {
		if event.Err != nil {
			return fmt.Errorf("watch error during restart: %w", event.Err)
		}
		if event.Type == watch.Deleted {
			break
		}
	}

	if err := m.CreatePod(ctx, podID); err != nil {
		return fmt.Errorf("error creating pod during restart: %w", err)
	}

	return nil
}

// PodEvent represents a pod state change event
type PodEvent struct {
	Type watch.EventType
	Pod  *corev1.Pod
	Err  error
}

// WatchPod returns a channel that emits pod events.
// Returns an error if the pod doesn't exist.
// The channel is closed when the context is cancelled or the watch ends.
// Caller is responsible for consuming events from the channel.
func (m *Manager) WatchPod(ctx context.Context, podID PodID) (<-chan PodEvent, error) {
	// Check if pod exists first
	_, err := m.GetPod(ctx, podID)
	if err != nil {
		return nil, fmt.Errorf("pod %s not found: %w", podID.Name(), err)
	}

	eventCh := make(chan PodEvent)

	go func() {
		defer close(eventCh)

		podName := podID.Name()

		// Set up watch for the specific pod
		watcher, err := m.clientset.CoreV1().Pods(m.agentNamespace).Watch(ctx, metav1.ListOptions{
			FieldSelector:  fmt.Sprintf("metadata.name=%s", podName),
			TimeoutSeconds: func() *int64 { t := int64(300); return &t }(), // 5 minute timeout
		})
		if err != nil {
			eventCh <- PodEvent{Err: fmt.Errorf("failed to create watcher for pod %s: %w", podName, err)}
			return
		}
		defer watcher.Stop()

		for {
			select {
			case <-ctx.Done():
				eventCh <- PodEvent{Err: ctx.Err()}
				return
			case event, ok := <-watcher.ResultChan():
				if !ok {
					eventCh <- PodEvent{Err: fmt.Errorf("watch channel closed for pod %s", podName)}
					return
				}

				switch event.Type {
				case watch.Added, watch.Modified, watch.Deleted:
					pod, ok := event.Object.(*corev1.Pod)
					if ok {
						eventCh <- PodEvent{Type: event.Type, Pod: pod}
					}
				case watch.Error:
					eventCh <- PodEvent{Err: fmt.Errorf("watch error for pod %s", podName)}
					return
				}
			}
		}
	}()

	return eventCh, nil
}

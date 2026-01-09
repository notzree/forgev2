package k8s

import (
	"fmt"
)

func UserIDLabel(userID string) string {
	return fmt.Sprintf("user-id=%s", userID)
}
func AgentIDLabel(agentID string) string {
	return fmt.Sprintf("agent-id=%s", agentID)
}

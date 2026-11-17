package evaluations

import "github.com/superduck-ai/open-managed-agents/internal/db"

const (
	taskRunning   = "running"
	taskCompleted = "completed"
	taskFailed    = "failed"
	taskCancelled = "cancelled"

	itemPending   = "pending"
	itemRunning   = "running"
	itemCompleted = "completed"
	itemFailed    = "failed"
	itemCancelled = "cancelled"

	scoreCompleted = "completed"
	scoreFailed    = "failed"
)

func taskClosed(status string) bool {
	return status == taskCompleted || status == taskFailed || status == taskCancelled
}

func taskRetryable(status string) bool {
	return status == taskCompleted || status == taskFailed
}

func itemOpen(status string) bool {
	return status == itemPending || status == itemRunning
}

func roundStatus(items []db.EvaluationTaskItem) string {
	for _, item := range items {
		if item.Status != itemFailed && item.Status != itemCancelled {
			return taskCompleted
		}
	}
	return taskFailed
}

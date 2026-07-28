package environments

import (
	"context"
	"testing"
	"time"
)

func TestRunSandboxCleanupStepProvidesFreshBoundedContext(t *testing.T) {
	err := runSandboxCleanupStep(func(cleanupCtx context.Context) error {
		if err := cleanupCtx.Err(); err != nil {
			t.Fatalf("cleanup context starts canceled: %v", err)
		}
		deadline, ok := cleanupCtx.Deadline()
		if !ok {
			t.Fatal("cleanup context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > sandboxCleanupStepTimeout {
			t.Fatalf(
				"cleanup context remaining budget = %s, want (0, %s]",
				remaining,
				sandboxCleanupStepTimeout,
			)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("runSandboxCleanupStep() error = %v", err)
	}
}

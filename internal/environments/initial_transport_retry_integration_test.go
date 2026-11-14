//go:build integration

package environments

import (
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

func TestInitialWorkerTransportFailureRequeuesExistingWork(t *testing.T) {
	for _, state := range []string{"transport", "permanent", "terminated", "archived", "stopped work"} {
		t.Run(state, func(t *testing.T) {
			database := preparationTestDatabase(t)
			work := preparationTestWork(t, database)
			cause := codesessions.ErrWorkerEventUnavailable
			switch state {
			case "permanent":
				cause = errors.New("invalid runtime configuration")
			case "terminated":
				if _, err := database.SQLDB().ExecContext(t.Context(), `UPDATE sessions SET status='terminated' WHERE uuid=$1`, work.SessionUUID); err != nil {
					t.Fatal(err)
				}
			case "archived":
				if _, err := database.SQLDB().ExecContext(t.Context(), `UPDATE sessions SET archived_at=NOW() WHERE uuid=$1`, work.SessionUUID); err != nil {
					t.Fatal(err)
				}
			case "stopped work":
				if _, err := database.StopEnvironmentWork(t.Context(), work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID, true); err != nil {
					t.Fatal(err)
				}
			}
			runner := &Runner{db: database, logger: logging.LoggerOrDefault(nil)}
			before := time.Now()
			runner.failWorkBeforeSandbox(t.Context(), work, cause)
			current, err := database.GetEnvironmentWork(t.Context(), work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			if state == "transport" {
				if current.State != "queued" || current.UUID != work.UUID || current.ClaimExpiresAt == nil || current.ClaimExpiresAt.Before(before.Add(managedAgentRecoveryRetryDelay)) {
					t.Fatalf("首次传输故障应延迟重试原Work: %+v", current)
				}
			} else if current.State != "stopped" {
				t.Fatalf("非重试场景不能重新排队: state=%s work=%s", state, current.State)
			}
		})
	}
}

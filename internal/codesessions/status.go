package codesessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *Service) syncPublicSessionFromWorker(ctx context.Context, record db.CodeSession, workerStatus string) error {
	if record.SessionExternalID == "" {
		return nil
	}
	// Startup/repeated idle is a runtime state, not completion of accepted work.
	// Keep the durable marker after completion so failed publication can retry.
	if workerStatus == "idle" && !record.WorkerTurnStarted {
		return nil
	}
	eventType, ok := publicEventTypeFromWorkerStatus(workerStatus)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	payload := map[string]any{
		"id":   stablePublicEventID(record.ExternalID, "worker_status_"+workerStatus+"\x00"+now.Format(time.RFC3339Nano)),
		"type": eventType, "created_at": formatTime(now), "processed_at": formatTime(now),
	}
	if workerStatus == "requires_action" {
		payload["stop_reason"] = map[string]any{"type": "requires_action"}
	}
	normalizePublicIdleStopReason(payload)
	raw, err := marshalRaw(payload)
	if err != nil {
		return err
	}
	return s.publishWorkerPublicPayloads(ctx, record.ExternalID, []json.RawMessage{raw})
}

func publicEventTypeFromWorkerStatus(status string) (string, bool) {
	switch status {
	case "running":
		return "session.status_running", true
	case "idle", "requires_action":
		return "session.status_idle", true
	default:
		return "", false
	}
}

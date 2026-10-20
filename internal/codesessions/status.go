package codesessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func (s *Service) syncPublicSessionFromWorker(ctx context.Context, record db.CodeSession, workerStatus string) error {
	if record.SessionExternalID == "" || workerStatus == "idle" {
		return nil
	}
	var requests []string
	if workerStatus == "requires_action" {
		var err error
		requests, err = maevents.PendingToolEventIDs(record.WorkerExternalMetadata, "", "")
		if err != nil {
			return err
		}
		pending, err := s.modelToolTurnPending(ctx, record)
		if err != nil || (pending && len(requests) == 0) {
			return err
		}
	}
	eventType, ok := publicEventTypeFromWorkerStatus(workerStatus)
	if !ok {
		return nil
	}
	// Each report is a new fact; the session event transaction drops unchanged status.
	eventID, err := ids.New("sevt_")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	payload := workerStatusPublicPayload(eventType, eventID, now, requests)
	raw, err := marshalRaw(payload)
	if err != nil {
		return err
	}
	return s.publishWorkerPublicPayloads(ctx, record.ExternalID, []json.RawMessage{raw})
}

func workerStatusPublicPayload(eventType, eventID string, now time.Time, pending []string) map[string]any {
	payload := map[string]any{
		"id":   eventID,
		"type": eventType, "created_at": formatTime(now), "processed_at": formatTime(now),
	}
	if eventType == "session.status_idle" {
		reason := map[string]any{"type": "requires_action"}
		if len(pending) > 0 {
			reason["event_ids"] = pending
		}
		payload["stop_reason"] = reason
	}
	normalizePublicIdleStopReason(payload)
	return payload
}

func publicEventTypeFromWorkerStatus(status string) (string, bool) {
	switch status {
	case "running":
		return "session.status_running", true
	case "requires_action":
		return "session.status_idle", true
	default:
		return "", false
	}
}

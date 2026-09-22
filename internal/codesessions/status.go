package codesessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
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
	payloads, err := s.publicSessionStatusPayloads(ctx, record, eventType, workerStatus)
	if err != nil {
		return err
	}
	return s.publishWorkerPublicPayloads(ctx, record.ExternalID, payloads)
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

func (s *Service) publicSessionStatusPayloads(ctx context.Context, record db.CodeSession, eventType, workerStatus string) ([]json.RawMessage, error) {
	status, ok := maevents.SessionStatus(eventType)
	if !ok {
		return nil, nil
	}
	session, found, err := s.db.GetSession(ctx, record.WorkspaceUUID, record.SessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	if status == "running" || session.Status == status {
		primary, found, err := s.db.GetPrimarySessionThread(ctx, record.WorkspaceUUID, record.SessionExternalID)
		if err != nil {
			return nil, err
		}
		if found && status == "running" {
			pending, err := s.PendingToolEventIDs(ctx, record.ExternalID, primary.ExternalID)
			if err != nil {
				return nil, err
			}
			if len(pending) > 0 {
				return nil, nil
			}
		}
		if session.Status == status && (!found || primary.Status == status) {
			return nil, nil
		}
	}
	now := time.Now().UTC()
	eventID := stablePublicEventID(record.ExternalID, "worker_status_"+status+"\x00"+session.UpdatedAt.UTC().Format(time.RFC3339Nano))
	payload := map[string]any{
		"id":           eventID,
		"type":         eventType,
		"created_at":   formatTime(now),
		"processed_at": formatTime(now),
	}
	if status == "idle" {
		pending, err := s.PendingToolEventIDs(ctx, record.ExternalID, "")
		if err != nil {
			return nil, err
		}
		if len(pending) > 0 {
			payload["stop_reason"] = map[string]any{"type": "requires_action", "event_ids": pending}
		}
		if workerStatus == "requires_action" && len(pending) == 0 {
			return nil, nil
		}
	}

	normalizePublicIdleStopReason(payload)
	raw, err := marshalRaw(payload)
	if err != nil {
		return nil, err
	}
	return []json.RawMessage{raw}, nil
}

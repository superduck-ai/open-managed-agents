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
	eventType, ok := publicEventTypeFromWorkerStatus(workerStatus)
	if !ok {
		return nil
	}
	if err := s.publishPublicSessionStatus(ctx, record, eventType); err != nil {
		return err
	}
	s.reconcileSubagentEvents(ctx, record.ExternalID)
	return nil
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

func (s *Service) publishPublicSessionStatus(ctx context.Context, record db.CodeSession, eventType string) error {
	status, ok := maevents.SessionStatus(eventType)
	if !ok {
		return nil
	}
	session, found, err := s.db.GetSession(ctx, record.WorkspaceUUID, record.SessionExternalID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if session.Status == status {
		thread, found, err := s.db.GetPrimarySessionThread(ctx, record.WorkspaceUUID, record.SessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if thread.Status == status {
			return nil
		}
	}
	now := time.Now().UTC()
	eventID := stablePublicEventID(record.ExternalID, "worker_status_"+status+"\x00"+session.UpdatedAt.UTC().Format(time.RFC3339Nano))
	payload, err := marshalRaw(map[string]any{
		"id":           eventID,
		"type":         eventType,
		"created_at":   formatTime(now),
		"processed_at": formatTime(now),
	})
	if err != nil {
		return err
	}
	return s.publishPublicPayloads(ctx, record.ExternalID, []json.RawMessage{payload})
}

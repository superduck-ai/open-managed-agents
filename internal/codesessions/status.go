package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func (s *Service) syncPublicSessionStatusFromWorker(ctx context.Context, record db.CodeSession, workerStatus string) error {
	if record.SessionExternalID == "" {
		return nil
	}
	eventType, ok := publicEventTypeFromWorkerStatus(workerStatus)
	if !ok {
		return nil
	}
	return s.publishPublicSessionStatus(ctx, record, eventType)
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
		thread, err := s.db.GetPrimarySessionThread(ctx, record.WorkspaceUUID, record.SessionExternalID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil
			}
			return err
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

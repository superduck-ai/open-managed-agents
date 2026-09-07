package codesessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

// UpdateWorkerState commits the worker snapshot and public status together.
func (s *Service) UpdateWorkerState(ctx context.Context, codeSessionID string, input db.UpdateCodeSessionWorkerStateInput) (db.CodeSession, error) {
	if s.sink == nil {
		return db.CodeSession{}, ErrPublicEventSinkUnavailable
	}
	record, found, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return db.CodeSession{}, err
	}
	if !found {
		return db.CodeSession{}, db.ErrNotFound
	}
	var updated db.CodeSession
	var created []db.SessionEvent
	err = s.db.WithManagedAgentEventTx(ctx, func(tx db.ManagedAgentEventTx) error {
		session, err := tx.LockSessionForEvents(ctx, record.WorkspaceUUID, record.SessionExternalID)
		if err != nil {
			return err
		}
		if session.ArchivedAt != nil {
			return db.ErrInvalidState
		}
		worker, err := tx.LockPublicEventWorker(ctx, session, db.SessionEventWorker{CodeSessionUUID: record.UUID, Epoch: input.WorkerEpoch})
		if err != nil {
			return err
		}
		updated, err = tx.UpdateCodeSessionWorkerState(ctx, worker, input)
		if err != nil || input.WorkerStatus == nil {
			return err
		}
		payloads, err := publicSessionStatusPayloads(ctx, tx, session, worker, *input.WorkerStatus)
		if err != nil {
			return err
		}
		created, err = s.sink.AppendCodeSessionEvents(ctx, tx, session, codeSessionID, payloads)
		if err != nil {
			return err
		}
		subagentPayloads, err := s.subagentPublicPayloads(ctx, tx, worker)
		if err != nil {
			return err
		}
		materialized, err := s.sink.AppendCodeSessionEvents(ctx, tx, session, codeSessionID, subagentPayloads)
		created = append(created, materialized...)
		return err
	})
	if err != nil {
		return db.CodeSession{}, err
	}
	s.sink.NotifyCodeSessionEvents(ctx, created)
	return updated, nil
}

func publicEventTypeFromWorkerStatus(status string) (string, bool) {
	switch status {
	case "running":
		return "session.thread_status_running", true
	case "idle", "requires_action":
		return "session.thread_status_idle", true
	default:
		return "", false
	}
}

// Compare the primary thread under the Session lock. A running child may keep
// the aggregate Session running even after the primary has become idle.
func publicSessionStatusPayloads(ctx context.Context, tx db.ManagedAgentEventTx, session db.Session, worker db.CodeSession, workerStatus string) ([]json.RawMessage, error) {
	eventType, ok := publicEventTypeFromWorkerStatus(workerStatus)
	if !ok {
		return nil, nil
	}
	status, _ := maevents.ThreadStatus(eventType)
	thread, found, err := tx.GetPrimarySessionThread(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	if thread.Status == status {
		return nil, nil
	}
	eventID := stablePublicEventID(worker.ExternalID, "worker_thread_status_"+status+"\x00"+thread.UpdatedAt.UTC().Format(time.RFC3339Nano))
	fields := map[string]any{"id": eventID, "type": eventType, "session_thread_id": thread.ExternalID}
	normalizePublicIdleStopReason(fields)
	payload, err := marshalRaw(fields)
	if err != nil {
		return nil, err
	}
	return []json.RawMessage{payload}, nil
}

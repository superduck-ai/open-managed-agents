package codesessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

const publicEventOrderingGap = time.Microsecond

func (s *Service) syncPublicSessionFromWorker(ctx context.Context, record db.CodeSession, workerStatus string) error {
	if record.SessionExternalID == "" {
		return nil
	}
	eventType, ok := publicEventTypeFromWorkerStatus(workerStatus)
	if !ok {
		return nil
	}
	payloads, err := s.publicSessionStatusPayloads(ctx, record, eventType)
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

// 把 worker 内部状态同步成外部可消费的 session 状态事件，并在 session 和主 thread 都已经处于目标状态时跳过重复发布。
// worker 报告 running，需要发布事件
//
//	比如：
//	- workerStatus = "running"
//	- publicEventTypeFromWorkerStatus 把它映射为 eventType = "session.status_running"
//	- maevents.SessionStatus("session.status_running") 返回 status = "running"
func (s *Service) publicSessionStatusPayloads(ctx context.Context, record db.CodeSession, eventType string) ([]json.RawMessage, error) {
	status, ok := maevents.SessionStatus(eventType)
	if !ok {
		return nil, nil
	}
	// 从 db 查 session status
	session, found, err := s.db.GetSession(ctx, record.WorkspaceUUID, record.SessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	// 状态相同，则查主 Agent 的状态
	if session.Status == status {
		thread, found, err := s.db.GetPrimarySessionThread(ctx, record.WorkspaceUUID, record.SessionExternalID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		// 如果主 Agent 也状态相同，则不需要重复发布
		if thread.Status == status {
			return nil, nil
		}
	}
	//
	return statusTransitionPayloads(record, session, eventType, status, time.Now().UTC())
}

func statusTransitionPayloads(
	record db.CodeSession,
	session db.Session,
	eventType string,
	status string,
	now time.Time,
) ([]json.RawMessage, error) {
	eventID := stablePublicEventID(record.ExternalID, "worker_status_"+status+"\x00"+session.UpdatedAt.UTC().Format(time.RFC3339Nano))
	payload, err := marshalRaw(map[string]any{
		"id":           eventID,
		"type":         eventType,
		"created_at":   formatTime(now),
		"processed_at": formatTime(now),
	})
	if err != nil {
		return nil, err
	}
	if status != "idle" {
		return []json.RawMessage{payload}, nil
	}
	usagePayload, err := sessionUsagePayload(record, session, now.Add(-publicEventOrderingGap))
	if err != nil {
		return nil, err
	}
	return []json.RawMessage{usagePayload, payload}, nil
}

func sessionUsagePayload(record db.CodeSession, session db.Session, at time.Time) (json.RawMessage, error) {
	eventID := stablePublicEventID(record.ExternalID, "session_usage\x00"+session.UpdatedAt.UTC().Format(time.RFC3339Nano))
	usage := json.RawMessage(`{}`)
	if len(session.Usage) > 0 {
		usage = session.Usage
	}
	return marshalRaw(map[string]any{
		"id":           eventID,
		"type":         "session.usage",
		"created_at":   formatTime(at),
		"processed_at": formatTime(at),
		"usage":        usage,
		"budget":       json.RawMessage(`null`),
	})
}

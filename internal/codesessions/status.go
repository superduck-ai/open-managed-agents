package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *Service) syncPublicSessionStatusFromWorker(ctx context.Context, record db.CodeSession, workerStatus string) error {
	// worker 状态属于内部执行协议；这里只把可公开表达的状态同步到 session 和主线程。
	// 找不到公开 session/thread 代表旧数据或尚未完成初始化，不应让 worker 上报失败。
	if record.SessionExternalID == "" {
		return nil
	}
	if workerStatus == "running" {
		return s.publishPublicRunningStatus(ctx, record)
	}
	if workerStatus != "idle" && workerStatus != "requires_action" {
		return nil
	}
	if err := s.db.SetSessionStatus(ctx, record.WorkspaceUUID, record.SessionExternalID, "idle"); err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	thread, err := s.db.GetPrimarySessionThread(ctx, record.WorkspaceUUID, record.SessionExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.db.SetSessionThreadStatus(ctx, record.WorkspaceUUID, record.SessionExternalID, thread.ExternalID, "idle"); err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	return nil
}

func (s *Service) publishPublicRunningStatus(ctx context.Context, record db.CodeSession) error {
	session, found, err := s.db.GetSession(ctx, record.WorkspaceUUID, record.SessionExternalID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if session.Status == "running" {
		return nil
	}
	now := time.Now().UTC()
	eventID := stablePublicEventID(record.ExternalID, "worker_status_running\x00"+session.UpdatedAt.UTC().Format(time.RFC3339Nano))
	payload, err := marshalRaw(map[string]any{
		"id":           eventID,
		"type":         "session.status_running",
		"created_at":   formatTime(now),
		"processed_at": formatTime(now),
	})
	if err != nil {
		return err
	}
	return s.publishPublicPayloads(ctx, record.ExternalID, []json.RawMessage{payload})
}

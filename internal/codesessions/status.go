package codesessions

import (
	"context"
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *Service) syncPublicSessionStatusFromWorker(ctx context.Context, record db.CodeSession, workerStatus string) error {
	// worker 状态属于内部执行协议；这里只把可公开表达的状态同步到 session 和主线程。
	// 找不到公开 session/thread 代表旧数据或尚未完成初始化，不应让 worker 上报失败。
	publicStatus, ok := publicSessionStatusFromWorkerStatus(workerStatus)
	if !ok || strings.TrimSpace(record.SessionExternalID) == "" {
		return nil
	}
	if err := s.db.SetSessionStatus(ctx, record.WorkspaceID, record.SessionExternalID, publicStatus); err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	thread, err := s.db.GetPrimarySessionThread(ctx, record.WorkspaceID, record.SessionExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.db.SetSessionThreadStatus(ctx, record.WorkspaceID, record.SessionExternalID, thread.ExternalID, publicStatus); err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	return nil
}

func publicSessionStatusFromWorkerStatus(workerStatus string) (string, bool) {
	// requires_action 对公开 API 表示“等待用户输入”，因此映射为 idle；其余内部状态不外泄。
	switch workerStatus {
	case "running":
		return "running", true
	case "idle", "requires_action":
		return "idle", true
	default:
		return "", false
	}
}

//go:build integration

package db

import (
	"encoding/json/v2"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestSessionControlRequestsPostgres(t *testing.T) {
	for _, kind := range []string{"user.tool_confirmation", "user.custom_tool_result"} {
		t.Run(kind, func(t *testing.T) {
			store, database := newTranscriptArchiveTestDB(t)
			scope := transcriptTestScope()
			sid := seedTranscriptArchiveSession(t, database, scope)
			session, found, err := store.GetSession(t.Context(), scope.WorkspaceUUID, "ses_"+sid)
			if err != nil || !found {
				t.Fatalf("控制输入夹具会话不存在: %v", err)
			}
			thread := "sthr_control"
			if _, err := store.CreateSessionThreadIfAbsent(t.Context(), SessionThread{UUID: uuid.NewV4().String(), ExternalID: thread, OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID, SessionExternalID: session.ExternalID, AgentSnapshot: session.AgentSnapshot, Status: "idle", Usage: []byte(`{}`), Stats: []byte(`{}`), CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			target := "sevt_pending_control"
			metadata, _ := json.Marshal(map[string]any{"managed_agent_tool_permission_request:" + target: map[string]string{"public_event_id": target, "request_id": "request", "provider_tool_use_id": "provider", "session_thread_id": thread}})
			if _, err := database.ExecContext(t.Context(), `UPDATE code_sessions SET worker_status='requires_action',worker_external_metadata=$1::jsonb WHERE workspace_uuid=$2 AND session_uuid=$3`, string(metadata), session.WorkspaceUUID, session.UUID); err != nil {
				t.Fatal(err)
			}
			newEvent := func(opposite bool) SessionEvent {
				id := uuid.NewV4().String()
				body := map[string]any{"id": "sevt_" + id, "type": kind, "session_thread_id": thread}
				if kind == "user.tool_confirmation" {
					body["tool_use_id"] = target
					body["result"] = "allow"
					if opposite {
						body["result"] = "deny"
					}
				} else {
					body["custom_tool_use_id"] = target
					body["content"] = []map[string]string{{"type": "text", "text": "原始答案"}}
					if opposite {
						body["content"] = []map[string]string{{"type": "text", "text": "相反答案"}}
					}
				}
				payload, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				return SessionEvent{UUID: id, ExternalID: "sevt_" + id, OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID, SessionExternalID: session.ExternalID, ThreadExternalID: &thread, EventType: kind, ToolUseID: &target, Payload: payload, CreatedAt: time.Now()}
			}
			if _, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{newEvent(false), newEvent(true)}, nil, SessionInputRequest{Key: "duplicate-batch", Fingerprint: "batch"}); !errors.Is(err, ErrSessionInputConflict) {
				t.Fatalf("同一批次不能同时接受同一目标的两份答案: %v", err)
			}
			request := SessionInputRequest{Key: "card-control-v1-test", Fingerprint: "original"}
			var mu sync.Mutex
			created, replayed := 0, 0
			ids := []string{}
			var wg sync.WaitGroup
			for range 8 {
				event := newEvent(false)
				wg.Go(func() {
					result, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{event}, nil, request)
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					defer mu.Unlock()
					if result.Replayed {
						replayed++
					} else {
						created++
					}
					for _, row := range result.Events {
						if row.EventType == kind {
							ids = append(ids, row.ExternalID)
						}
					}
				})
			}
			wg.Wait()
			if created != 1 || replayed != 7 || len(ids) != 8 {
				t.Fatalf("并发控制输入应只有一次新接受: created=%d replayed=%d ids=%v", created, replayed, ids)
			}
			for _, id := range ids {
				if id != ids[0] {
					t.Fatalf("重复控制输入未复用原事件ID: %v", ids)
				}
			}
			var count int
			if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM session_events WHERE workspace_uuid=$1 AND session_uuid=$2 AND event_type=$3`, session.WorkspaceUUID, session.UUID, kind).Scan(&count); err != nil || count != 1 {
				t.Fatalf("不能产生重复控制事实: count=%d err=%v", count, err)
			}
			conflict := request
			conflict.Fingerprint = "opposite"
			if _, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{newEvent(true)}, nil, conflict); !errors.Is(err, ErrSessionInputRequestConflict) {
				t.Fatalf("相反结果不能覆盖原控制请求: %v", err)
			}
			var workerID string
			if err := database.QueryRowContext(t.Context(), `SELECT external_id FROM code_sessions WHERE workspace_uuid=$1 AND session_uuid=$2`, session.WorkspaceUUID, session.UUID).Scan(&workerID); err != nil {
				t.Fatal(err)
			}
			for i := range 8 {
				event := newEvent(true)
				if i%2 == 0 {
					event.ToolUseID = new(maevents.ToolUseEventID(workerID, "provider", thread))
				}
				wg.Go(func() {
					other := SessionInputRequest{Key: "another-entry-" + event.ExternalID, Fingerprint: "opposite"}
					if _, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{event}, nil, other); !errors.Is(err, ErrSessionInputConflict) {
						t.Errorf("不同入口不能重复接受同一工具的控制结果: %v", err)
					}
				})
			}
			wg.Wait()
			if _, err := database.ExecContext(t.Context(), `UPDATE code_sessions SET worker_external_metadata='{}' WHERE workspace_uuid=$1 AND session_uuid=$2`, session.WorkspaceUUID, session.UUID); err != nil {
				t.Fatal(err)
			}
			result, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{newEvent(false)}, nil, request)
			if err != nil || !result.Replayed || len(result.Events) == 0 {
				t.Fatalf("待处理状态消失后仍应返回原回执: %+v %v", result, err)
			}
		})
	}
}

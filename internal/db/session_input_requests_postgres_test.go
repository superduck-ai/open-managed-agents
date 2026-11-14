//go:build integration

package db

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"
)

func TestSessionInputRequestsPostgres(t *testing.T) {
	store, database := newTranscriptArchiveTestDB(t)
	scope := transcriptTestScope()
	sid := seedTranscriptArchiveSession(t, database, scope)
	session, found, err := store.GetSession(t.Context(), scope.WorkspaceUUID, "ses_"+sid)
	if err != nil || !found {
		t.Fatalf("验收会话不存在: %v", err)
	}
	if _, err := store.CreateSessionThreadIfAbsent(t.Context(), SessionThread{
		UUID: uuid.NewV4().String(), ExternalID: "sthr_input_test", OrganizationUUID: session.OrganizationUUID,
		WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID, SessionExternalID: session.ExternalID,
		AgentSnapshot: session.AgentSnapshot, Status: "idle", Usage: []byte(`{}`), Stats: []byte(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	newEvent := func() SessionEvent {
		id := uuid.NewV4().String()
		payload, err := json.Marshal(struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Content string `json:"content"`
		}{"sevt_" + id, "user.message", "幂等输入"})
		if err != nil {
			t.Fatal(err)
		}
		return SessionEvent{UUID: id, ExternalID: "sevt_" + id, OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID, SessionExternalID: session.ExternalID, EventType: "user.message", Payload: payload, CreatedAt: time.Now().UTC()}
	}
	request := SessionInputRequest{Key: "rollback", Fingerprint: "original"}
	if _, err := database.ExecContext(t.Context(), `ALTER TABLE session_input_requests ADD CONSTRAINT test_receipt_rollback CHECK(request_key <> 'rollback')`); err != nil {
		t.Fatal(err)
	}
	failed := newEvent()
	if _, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{failed}, nil, request); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatal("回执保存失败时事件事务必须回滚")
	}
	if _, err := store.GetSessionEvent(t.Context(), session.WorkspaceUUID, session.ExternalID, failed.ExternalID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("回执失败后残留输入: %v", err)
	}
	if _, found, err := store.FindSessionInputRequest(t.Context(), session, request); err != nil || found {
		t.Fatalf("回滚后残留回执: %v %v", found, err)
	}
	if _, err := database.ExecContext(t.Context(), `ALTER TABLE session_input_requests DROP CONSTRAINT test_receipt_rollback`); err != nil {
		t.Fatal(err)
	}
	first := newEvent()
	changes, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{first}, nil, request)
	if err != nil || changes.Replayed || len(changes.Events) == 0 {
		t.Fatalf("首次请求未保存: %+v %v", changes, err)
	}
	conflict := request
	conflict.Fingerprint = "different"
	if _, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{newEvent()}, nil, conflict); !errors.Is(err, ErrSessionInputRequestConflict) {
		t.Fatalf("同标识不同输入未拒绝: %v", err)
	}
	for _, field := range []string{"组织", "工作区", "会话"} {
		wrong := session
		switch field {
		case "组织":
			wrong.OrganizationUUID = uuid.NewV4().String()
		case "工作区":
			wrong.WorkspaceUUID = uuid.NewV4().String()
		case "会话":
			wrong.UUID = uuid.NewV4().String()
		}
		if _, found, err := store.FindSessionInputRequest(t.Context(), wrong, request); err != nil || found {
			t.Fatalf("%s越界读取: %v %v", field, found, err)
		}
	}
	replay, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{newEvent()}, nil, request)
	if err != nil || !replay.Replayed || len(replay.Events) != len(changes.Events) {
		t.Fatalf("未返回原事件: %+v %v", replay, err)
	}
	for i, event := range changes.Events {
		if event.ExternalID != replay.Events[i].ExternalID {
			t.Fatalf("重试创建了新事件: %+v", replay.Events)
		}
	}
	wrong := session
	wrong.WorkspaceUUID = uuid.NewV4().String()
	if err := store.CompleteSessionInputRequestHandoff(t.Context(), wrong, request); !errors.Is(err, ErrNotFound) {
		t.Fatalf("越界确认交接: %v", err)
	}
	if err := store.CompleteSessionInputRequestHandoff(t.Context(), session, conflict); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不同指纹可以确认原交接: %v", err)
	}
	if replay.InputHandoffCompleted {
		t.Fatal("首次交接未经确认不能标记完成")
	}
	if err := store.CompleteSessionInputRequestHandoff(t.Context(), session, request); err != nil {
		t.Fatal(err)
	}
	confirmed, found, err := store.FindSessionInputRequest(t.Context(), session, request)
	if err != nil || !found || !confirmed.InputHandoffCompleted {
		t.Fatalf("交接确认没有保存: %+v %v", confirmed, err)
	}
	request = SessionInputRequest{Key: "concurrent", Fingerprint: "same"}
	var mu sync.Mutex
	var wg sync.WaitGroup
	created, replays := 0, 0
	for range 8 {
		event := newEvent()
		wg.Go(func() {
			result, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{event}, nil, request)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if result.Replayed {
				replays++
			} else {
				created++
			}
		})
	}
	wg.Wait()
	if created != 1 || replays != 7 {
		t.Fatalf("并发提交未去重: 首次=%d 重试=%d", created, replays)
	}
	empty := SessionInputRequest{Key: "empty", Fingerprint: "budget-filtered"}
	if result, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, nil, nil, empty); err != nil || result.Replayed || len(result.Events) != 0 {
		t.Fatalf("空回执未保存: %+v %v", result, err)
	}
	if result, err := store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{newEvent()}, nil, empty); err != nil || !result.Replayed || len(result.Events) != 0 {
		t.Fatalf("重试不能把预算拦截的原请求重新执行: %+v %v", result, err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE session_events SET processed_at=now() WHERE workspace_uuid=$1 AND session_uuid=$2 AND external_id=$3`, session.WorkspaceUUID, session.UUID, first.ExternalID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE sessions SET archived_at=now() WHERE workspace_uuid=$1 AND uuid=$2`, session.WorkspaceUUID, session.UUID); err != nil {
		t.Fatal(err)
	}
	request = SessionInputRequest{Key: "rollback", Fingerprint: "original"}
	replay, err = store.AppendSessionInputRequest(t.Context(), session.WorkspaceUUID, session.ExternalID, []SessionEvent{newEvent()}, nil, request)
	if err != nil || !replay.Replayed || slices.ContainsFunc(replay.WorkerInputEvents(), func(event SessionEvent) bool { return event.EventType == "user.message" }) {
		t.Fatalf("已处理/归档后重试不能再执行: %+v %v", replay, err)
	}
	var count int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM session_events WHERE workspace_uuid=$1 AND session_uuid=$2 AND event_type='user.message'`, session.WorkspaceUUID, session.UUID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("产生了重复输入: count=%d err=%v", count, err)
	}
}

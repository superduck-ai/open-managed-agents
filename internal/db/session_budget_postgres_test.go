package db

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"
)

func TestSessionBudgetTransitionPostgres(t *testing.T) {
	store, database := newTranscriptArchiveTestDB(t)
	scope := transcriptTestScope()
	sessionUUID := seedTranscriptArchiveSession(t, database, scope)
	sessionID := "ses_" + sessionUUID
	session, found, err := store.GetSession(t.Context(), scope.WorkspaceUUID, sessionID)
	if err != nil || !found {
		t.Fatalf("加载会话：%v", err)
	}
	session.Budget = json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"1","currency":"USD"}}`)
	session, err = store.UpdateSession(t.Context(), scope.WorkspaceUUID, sessionID, session)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewSessionThreadMapper(store.mapperDB).Insert(t.Context(), sessionThreadWriteParameters(SessionThread{
		UUID: uuid.NewV4().String(), ExternalID: "sthr_budget", OrganizationUUID: scope.OrganizationUUID,
		WorkspaceUUID: scope.WorkspaceUUID, SessionUUID: sessionUUID, SessionExternalID: sessionID,
		AgentSnapshot: json.RawMessage(`{}`), Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`),
		Status: "running", CreatedAt: time.Now().UTC(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	event := func(id, kind, payload string, at time.Time) SessionEvent {
		return SessionEvent{UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: scope.OrganizationUUID,
			WorkspaceUUID: scope.WorkspaceUUID, SessionUUID: sessionUUID, SessionExternalID: sessionID,
			EventType: kind, Payload: json.RawMessage(payload), CreatedAt: at, ProcessedAt: at}
	}
	usage := event("sevt_budget_usage", "session.usage", `{"type":"session.usage"}`, now)
	idle := event("sevt_budget_idle", "session.status_idle", `{"type":"session.status_idle","stop_reason":{"type":"budget_reached"}}`, now.Add(time.Microsecond))
	transition := SessionBudgetTransition{Budget: session.Budget, ReachedAt: now}
	broken := idle
	broken.Payload = json.RawMessage(`{`)
	if _, err := store.AppendSessionBudgetReachedEvents(t.Context(), scope.WorkspaceUUID, sessionID, []SessionEvent{usage, broken}, transition); err == nil {
		t.Fatal("非法事件没有导致事务失败")
	}
	session, _, err = store.GetSession(t.Context(), scope.WorkspaceUUID, sessionID)
	if err != nil || session.BudgetReachedAt != nil {
		t.Fatalf("触顶标记未回滚：%+v，%v", session.BudgetReachedAt, err)
	}
	if _, err := store.GetSessionEvent(t.Context(), scope.WorkspaceUUID, sessionID, usage.ExternalID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("用量事件未回滚：%v", err)
	}
	changes, err := store.AppendSessionBudgetReachedEvents(t.Context(), scope.WorkspaceUUID, sessionID, []SessionEvent{usage, idle}, transition)
	if err != nil || len(changes.Events) < 2 {
		t.Fatalf("重试未成功：%+v，%v", changes, err)
	}
	session, _, err = store.GetSession(t.Context(), scope.WorkspaceUUID, sessionID)
	if err != nil || session.BudgetReachedAt == nil || session.Status != "idle" {
		t.Fatalf("触顶状态未保存：%+v，%v", session, err)
	}
	changes, err = store.AppendSessionBudgetReachedEvents(t.Context(), scope.WorkspaceUUID, sessionID, []SessionEvent{usage, idle}, transition)
	if err != nil || len(changes.Events) != 0 {
		t.Fatalf("重复执行产生事件：%+v，%v", changes, err)
	}
	session.BudgetReachedAt = nil
	session.Budget = json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"100","currency":"USD"}}`)
	if _, err = store.UpdateSession(t.Context(), scope.WorkspaceUUID, sessionID, session); err != nil {
		t.Fatal(err)
	}
	changes, err = store.AppendSessionBudgetReachedEvents(t.Context(), scope.WorkspaceUUID, sessionID, []SessionEvent{usage, idle}, transition)
	if err != nil || len(changes.Events) != 0 {
		t.Fatalf("旧预算覆盖新预算：%+v，%v", changes, err)
	}
	session, _, err = store.GetSession(t.Context(), scope.WorkspaceUUID, sessionID)
	if err != nil || session.BudgetReachedAt != nil {
		t.Fatalf("新预算被旧触顶标记污染：%v", err)
	}
	metered := event("sevt_search_end", "span.model_request_end", `{"model_usage":{"server_tool_use":{"web_search_requests":3}},"billing":{"list_cost":"2","currency":"USD"}}`, now)
	if _, err := store.AppendSessionEvents(t.Context(), scope.WorkspaceUUID, sessionID, []SessionEvent{metered}, nil); err != nil {
		t.Fatal(err)
	}
	totals, err := store.SumSessionUsageTotals(t.Context(), scope.WorkspaceUUID, sessionID)
	if err != nil || totals.WebSearchRequests != 3 || totals.ListCostCents != 2 {
		t.Fatalf("搜索计量聚合错误：%+v，%v", totals, err)
	}
}

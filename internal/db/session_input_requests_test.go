package db

import (
	"slices"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestReplayedSessionInputsOnlyDispatchUnprocessedEvents(t *testing.T) {
	processed := time.Now()
	changes := SessionEventChanges{
		Replayed: true,
		Events: []SessionEvent{
			{ExternalID: "processed", EventType: "user.message", ProcessedAt: processed},
			{ExternalID: "pending", EventType: "user.message"},
			{ExternalID: "settled", EventType: "user.interrupt"},
			{ExternalID: "control", EventType: "user.custom_tool_result", ProcessedAt: processed},
		},
		SettledInputEventIDs: []string{"settled"},
	}
	queued := changes.WorkerInputEvents()
	if len(queued) != 2 || queued[0].ExternalID != "pending" || queued[1].ExternalID != "control" || len(changes.Events) != 4 {
		t.Fatalf("重试不能再次投递已处理输入，也不能漏掉待交接输入: %+v", queued)
	}
}

func TestSessionInputRequestMapperBindings(t *testing.T) {
	session := Session{OrganizationUUID: "org", WorkspaceUUID: "workspace", UUID: "session"}
	request := SessionInputRequest{Key: "request", Fingerprint: "fingerprint"}
	find := buildSessionInputRequestMapperFind(yourbatis.DialectPostgres, session, request)
	assertMapperSQLContains(t, find, "WHERE organization_uuid = $1 AND workspace_uuid = $2 AND session_uuid = $3 AND request_key = $4")
	assertMapperArgumentNames(t, find, []string{"session.OrganizationUUID", "session.WorkspaceUUID", "session.UUID", "request.Key"})
	row := sessionInputRequestRow{EventIDs: `["event"]`, SettledInputEventIDs: `[]`}
	insert := buildSessionInputRequestMapperInsert(yourbatis.DialectPostgres, session, request, row)
	assertMapperSQLContains(t, insert, "VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb)")
	assertMapperArgumentNames(t, insert, []string{"session.OrganizationUUID", "session.WorkspaceUUID", "session.UUID", "request.Key", "request.Fingerprint", "row.EventIDs", "row.SettledInputEventIDs"})
	completed := buildSessionInputRequestMapperCompleteHandoff(yourbatis.DialectPostgres, session, request)
	assertMapperSQLContains(t, completed, "WHERE organization_uuid = $1 AND workspace_uuid = $2 AND session_uuid = $3 AND request_key = $4 AND fingerprint = $5")
	assertMapperArgumentNames(t, completed, []string{"session.OrganizationUUID", "session.WorkspaceUUID", "session.UUID", "request.Key", "request.Fingerprint"})
	values := make([]any, 0, len(insert.Args))
	for _, arg := range insert.Args {
		values = append(values, arg.Value)
	}
	if !slices.Equal(values, []any{"org", "workspace", "session", "request", "fingerprint", `["event"]`, `[]`}) {
		t.Fatalf("回执参数顺序错误: %v", values)
	}
}

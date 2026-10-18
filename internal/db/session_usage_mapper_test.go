package db

import (
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionUsageMapperBoundScope(t *testing.T) {
	params := sessionUsageTotalsParams{WorkspaceUUID: "workspace", SessionExternalID: "session", ThreadExternalID: "thread"}
	bound := buildSessionUsageMapperSumUsageTotals(yourbatis.DialectPostgres, params)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sessionUsageMapperSumUsageTotalsStatement, bound: bound,
		wantID: "SessionUsageMapper.SumUsageTotals", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionExternalID", "params.ThreadExternalID"},
		wantSQLFragments:  []string{"e.workspace_uuid = $1", "e.session_external_id = $2", "e.thread_external_id = $3"},
	})
}

func TestSessionUsageCacheTokensPostgres(t *testing.T) {
	d, _ := newTranscriptArchiveTestDB(t)
	for _, tc := range []struct {
		name           string
		cache          string
		want5m, want1h int64
	}{
		{"非数字", `{"ephemeral_5m_input_tokens":"bad","ephemeral_1h_input_tokens":"bad"}`, 0, 0},
		{"错误类型", `{"ephemeral_5m_input_tokens":true,"ephemeral_1h_input_tokens":{}}`, 0, 0},
		{"负数和小数", `{"ephemeral_5m_input_tokens":-1,"ephemeral_1h_input_tokens":1.5}`, 0, 0},
		{"空值", `{"ephemeral_5m_input_tokens":null,"ephemeral_1h_input_tokens":""}`, 0, 0},
		{"缺字段", `{}`, 0, 0},
		{"整数", `{"ephemeral_5m_input_tokens":7,"ephemeral_1h_input_tokens":11}`, 7, 11},
		{"数字字符串", `{"ephemeral_5m_input_tokens":"13","ephemeral_1h_input_tokens":"17"}`, 13, 17},
		{"单字段异常", `{"ephemeral_5m_input_tokens":"bad","ephemeral_1h_input_tokens":19}`, 0, 19},
	} {
		for _, field := range []string{"model_usage", "usage"} {
			t.Run(tc.name+"/"+field, func(t *testing.T) {
				org, ws, session := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
				for _, cache := range []string{`{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":3}`, tc.cache} {
					now := time.Now().UTC()
					payload := []byte(fmt.Sprintf(`{"%s":{"input_tokens":5,"cache_creation":%s},"billing":{"list_cost":"1"}}`, field, cache))
					_, err := NewSessionEventMapper(d.mapperDB).Insert(t.Context(), sessionEventWriteParams{
						UUID: uuid.NewV4().String(), ExternalID: "event_" + uuid.NewV4().String(), OrganizationUUID: org, WorkspaceUUID: ws,
						SessionUUID: session, SessionExternalID: session, EventType: "span.model_request_end", Payload: payload, CreatedAt: now, ProcessedAt: now,
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				totals, err := d.SumSessionUsageTotals(t.Context(), ws, session)
				if err != nil {
					t.Fatal(err)
				}
				if totals.CacheWrite5mTokens != 2+tc.want5m || totals.CacheWrite1hTokens != 3+tc.want1h || totals.InputTokens != 10 || totals.ListCostCents != 2 {
					t.Fatalf("异常缓存字段影响聚合：%+v", totals)
				}
			})
		}
	}
}

//go:build integration

package db

import (
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"testing"
	"time"
	"uuid"
)

func TestModelPriceTTLReconciliationPostgres(t *testing.T) {
	d, _ := newTranscriptArchiveTestDB(t)
	now := time.Now().UTC()
	org, ws, session := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	rates := billing.Rates{Input: new("3"), Output: new("15"), CacheRead: new("0.3"), CacheWrite5m: new("3.75"), CacheWrite1h: new("6")}
	usage := billing.TokenUsage{InputTokens: 1000000, OutputTokens: 1000000, CacheReadInputTokens: 1000000, CacheCreationInputTokens: 2000000, CacheWrite5mTokens: 1000000, CacheWrite1hTokens: 1000000}
	cents, ok := rates.Cost(usage)
	if !ok || cents != 2805 {
		t.Fatalf("双 TTL 计费错误: %d", cents)
	}
	payload := []byte(`{"model":"priced","model_usage":{"input_tokens":1000000,"output_tokens":1000000,"cache_read_input_tokens":1000000,"cache_creation_input_tokens":2000000,"cache_creation":{"ephemeral_5m_input_tokens":1000000,"ephemeral_1h_input_tokens":1000000}},"billing":{"list_cost":"2805","currency":"USD"}}`)
	event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "event_ttl", OrganizationUUID: org, WorkspaceUUID: ws, SessionUUID: session, SessionExternalID: "sesn_ttl", EventType: "span.model_request_end", Payload: payload, CreatedAt: now}
	_, err := NewSessionEventMapper(d.mapperDB).Insert(t.Context(), sessionEventWriteParams{UUID: event.UUID, ExternalID: event.ExternalID, OrganizationUUID: org, WorkspaceUUID: ws, SessionUUID: session, SessionExternalID: event.SessionExternalID, EventType: event.EventType, Payload: payload, CreatedAt: now, ProcessedAt: new(now)})
	if err != nil {
		t.Fatal(err)
	}
	if err := insertSessionUsageRecord(t.Context(), d.mapperDB, event); err != nil {
		t.Fatal(err)
	}
	totals, err := d.SumSessionUsageTotals(t.Context(), ws, event.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := d.AggregateUsage(t.Context(), UsageQuery{OrganizationUUID: org, WorkspaceUUIDs: []string{ws}, Start: now.Add(-time.Hour), End: now.Add(time.Hour), AsOf: now.Add(time.Hour), Timezone: "UTC"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("账本聚合失败: %v", err)
	}
	if totals.ListCostCents != cents || rows[0].ModelCents != cents || totals.CacheWrite5mTokens != 1000000 || totals.CacheWrite1hTokens != 1000000 || rows[0].CacheWrite5mTokens != totals.CacheWrite5mTokens || rows[0].CacheWrite1hTokens != totals.CacheWrite1hTokens {
		t.Fatalf("对账失败: %+v %+v", totals, rows[0])
	}
}

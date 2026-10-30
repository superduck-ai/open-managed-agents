//go:build integration

package db

import (
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

func TestUsageRecordsPostgres(t *testing.T) {
	store, _ := newTranscriptArchiveTestDB(t)
	now := time.Date(2026, 10, 1, 16, 30, 0, 0, time.UTC)
	org, workspace := uuid.NewV4().String(), uuid.NewV4().String()
	query := UsageQuery{OrganizationUUID: org, WorkspaceUUIDs: []string{workspace}, Start: now.Add(-time.Hour), End: now.Add(time.Hour), AsOf: time.Now().Add(time.Hour), Timezone: "Asia/Shanghai"}
	record := UsageRecord{Finalized: true, OrganizationUUID: org, WorkspaceUUID: workspace, AttemptID: "attempt", Source: "api", Model: "model-a", StartedAt: now.Add(-time.Minute), EndedAt: now, InputTokens: 100, OutputTokens: 50, CacheReadTokens: 20, CacheWriteTokens: 30, ModelCents: new(int64(12)), SearchRequests: 2, SearchCents: 2, Complete: true, Version: "request_cents_v1"}
	err := store.mapperDB.Transaction(t.Context(), func(executor yourbatis.Executor) error {
		if err := NewUsageRecordMapper(executor).Insert(t.Context(), record); err != nil {
			return err
		}
		return ErrInvalidState
	})
	if err == nil {
		t.Fatal("事务未回滚")
	}
	rows, err := store.AggregateUsage(t.Context(), query)
	if err != nil || len(rows) != 0 {
		t.Fatalf("回滚后仍有计量：%+v %v", rows, err)
	}
	for range 2 {
		if err := store.InsertUsageRecord(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}
	rows, err = store.AggregateUsage(t.Context(), query)
	if err != nil || len(rows) != 1 {
		t.Fatalf("聚合失败：%+v %v", rows, err)
	}
	if rows[0].Requests != 1 || rows[0].Day != "2026-10-02" || rows[0].ModelCents != 12 || rows[0].SearchCents != 2 || rows[0].InputTokens != 100 {
		t.Fatalf("重复计费或跨日聚合错误：%+v", rows)
	}
	for _, mutate := range []func(*UsageQuery){
		func(q *UsageQuery) { q.OrganizationUUID = uuid.NewV4().String() },
		func(q *UsageQuery) { q.WorkspaceUUIDs = []string{uuid.NewV4().String()} },
		func(q *UsageQuery) { q.Model = "other-model" },
		func(q *UsageQuery) { q.Source = "workbench" },
		func(q *UsageQuery) { q.End = now },
		func(q *UsageQuery) { q.WorkspaceUUIDs = nil },
	} {
		filtered := query
		mutate(&filtered)
		rows, err := store.AggregateUsage(t.Context(), filtered)
		if err != nil || len(rows) != 0 {
			t.Fatalf("查询范围泄漏：%+v %v", rows, err)
		}
	}
	record.AttemptID, record.ModelCents, record.Complete = "unknown", nil, false
	if err := store.InsertUsageRecord(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	rows, err = store.AggregateUsage(t.Context(), query)
	if err != nil || len(rows) != 1 || rows[0].Unpriced != 1 || rows[0].Incomplete != 1 {
		t.Fatalf("未知费用未被区分：%+v %v", rows, err)
	}
	inflight := record
	inflight.AttemptID = "inflight"
	inflight.Finalized = false
	inflight.Model = "model-inflight"
	inflight.InputTokens = 999
	inflight.ModelCents = nil
	inflight.Complete = false
	if err := store.InsertUsageRecord(t.Context(), inflight); err != nil {
		t.Fatal(err)
	}
	rows, err = store.AggregateUsage(t.Context(), query)
	if err != nil || len(rows) != 1 || rows[0].Requests != 2 || rows[0].Unpriced != 1 || rows[0].Incomplete != 1 || rows[0].InputTokens != 200 {
		t.Fatalf("进行中的请求被计入聚合：%+v %v", rows, err)
	}
	dimensions, err := store.UsageDimensions(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	for _, dimension := range dimensions {
		if dimension.Model == inflight.Model {
			t.Fatalf("进行中的请求进入筛选项：%+v", dimensions)
		}
	}
}

func TestProviderCostFinalizationPostgres(t *testing.T) {
	store, database := newTranscriptArchiveTestDB(t)
	now := time.Now().UTC()
	record := UsageRecord{OrganizationUUID: uuid.NewV4().String(), WorkspaceUUID: uuid.NewV4().String(), AttemptID: "provider-cost", Source: "api", Model: "test/model", StartedAt: now, EndedAt: now, Version: "request_cents_v1"}
	if err := store.InsertUsageRecord(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	record.Finalized = true
	record.ModelCents = new(int64(100))
	record.ProviderCost = []byte(`{"amount":"7.2","currency":"CNY","exchange_rate":"7.2","usd_cents":100}`)
	if err := store.InsertUsageRecord(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	record.ModelCents = new(int64(200))
	if err := store.InsertUsageRecord(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	var amount string
	var cents int64
	if err := database.QueryRowContext(t.Context(), "SELECT provider_cost->>'amount', model_cents FROM usage_records WHERE attempt_id = 'provider-cost'").Scan(&amount, &cents); err != nil {
		t.Fatal(err)
	}
	if amount != "7.2" || cents != 100 {
		t.Fatalf("原币费用丢失或重复结算：%s %d", amount, cents)
	}
}

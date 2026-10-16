package db

import (
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"testing"
	"time"
	"uuid"
)

func TestModelPricesPostgres(t *testing.T) {
	d, _ := newTranscriptArchiveTestDB(t)
	org, ws := uuid.NewV4().String(), uuid.NewV4().String()
	_, err := d.CreateLLMProvider(t.Context(), LLMProvider{UUID: uuid.NewV4().String(), ExternalID: "provider_price", OrganizationUUID: org, WorkspaceUUID: ws, Name: "价格测试", SecretEnvelope: &secrets.Envelope{Ciphertext: []byte("test"), Nonce: []byte("test"), WrappedDEK: []byte("test"), FormatVersion: 1, KeyProvider: "test", KeyVersion: 1}, BaseURL: "http://localhost", ModelIDs: []string{"priced", "other", "ambiguous", "test/priced"}, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	price := CatalogPrice{Model: "test/priced", Original: []byte(`{}`), Price: billing.ModelPrice{Source: "openrouter", SourceModel: "test/priced", Version: "first", Rates: &billing.Rates{Input: new("3"), Output: new("15"), CacheRead: new("0.3"), CacheWrite5m: new("3.75"), CacheWrite1h: new("6")}}}
	if err := d.ReplaceModelPriceCatalog(t.Context(), "openrouter", nil); err == nil {
		t.Fatal("空目录覆盖了价格")
	}
	if err := d.ReplaceModelPriceCatalog(t.Context(), "openrouter", []CatalogPrice{price}); err != nil {
		t.Fatal(err)
	}
	scoped, err := d.BillingCalculator().Scope(t.Context(), org, ws)
	if err != nil {
		t.Fatal(err)
	}

	mapper := NewModelPriceCatalogMapper(d.mapperDB)
	for _, model := range []string{"unused/model", "test/other"} {
		if err := mapper.Save(t.Context(), modelPriceCatalogWrite{Source: "openrouter", Model: model, Price: []byte(`{"rates":4}`), Original: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	selected, err := d.BillingCalculator().Scope(t.Context(), org, ws, "priced")
	if err != nil || !selected.HasModel("priced") || selected.HasModel("other") {
		t.Fatalf("加载了请求之外的价格: %v", err)
	}
	for _, model := range []string{"a/ambiguous", "b/ambiguous"} {
		if err := mapper.Save(t.Context(), modelPriceCatalogWrite{Source: "openrouter", Model: model, Price: []byte(`{"rates":{"input":"1","output":"1"}}`), Original: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	ambiguous, err := d.BillingCalculator().Scope(t.Context(), org, ws, "ambiguous")
	if err != nil || ambiguous.HasModel("ambiguous") {
		t.Fatalf("歧义短名被计价: %v", err)
	}
	exact, err := d.BillingCalculator().Scope(t.Context(), org, ws, "test/priced")
	if err != nil || !exact.HasModel("test/priced") {
		t.Fatalf("完整模型 ID 匹配失败: %v", err)
	}

	price.Price.Rates.Input = new("5")
	price.Price.Version = "second"
	if err := d.ReplaceModelPriceCatalog(t.Context(), "openrouter", []CatalogPrice{price}); err != nil {
		t.Fatal(err)
	}
	if cents, ok := scoped.ModelRequestCents("priced", billing.TokenUsage{InputTokens: 1000000}); !ok || cents != 300 {
		t.Fatal("在途价格快照变化")
	}
	latest, err := d.BillingCalculator().Scope(t.Context(), org, ws)
	if err != nil {
		t.Fatal(err)
	}
	if cents, ok := latest.ModelRequestCents("priced", billing.TokenUsage{InputTokens: 1000000}); !ok || cents != 500 {
		t.Fatal("新价格未生效")
	}
	if err := d.ReplaceModelPriceCatalog(t.Context(), "openrouter", []CatalogPrice{price, price}); err == nil {
		t.Fatal("重复模型没有回滚")
	}
	latest, err = d.BillingCalculator().Scope(t.Context(), org, ws)
	if err != nil || !latest.HasModel("priced") {
		t.Fatal("失败同步丢失已有价格")
	}
	other, err := d.BillingCalculator().Scope(t.Context(), org, uuid.NewV4().String())
	if err != nil || other.HasModel("priced") {
		t.Fatal("跨工作区模型泄漏")
	}

}

// 非零双 TTL 夹具只写入隔离测试库，验证预算事件和看板账本使用同一金额。
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
	_, err := NewSessionEventMapper(d.mapperDB).Insert(t.Context(), sessionEventWriteParams{UUID: event.UUID, ExternalID: event.ExternalID, OrganizationUUID: org, WorkspaceUUID: ws, SessionUUID: session, SessionExternalID: event.SessionExternalID, EventType: event.EventType, Payload: payload, CreatedAt: now, ProcessedAt: now})
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

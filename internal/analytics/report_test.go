package analytics

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestParseQueryRejectsInvalidFilters(t *testing.T) {
	for _, raw := range []string{"timezone=unknown", "source=other", "starting_at=bad", "granularity=hourly", "starting_at=2024-01-01T00:00:00Z&ending_at=2026-01-01T00:00:00Z"} {
		values, _ := url.ParseQuery(raw)
		if _, err := ParseQuery(values, time.Now()); err == nil {
			t.Fatalf("非法查询被接受：%s", raw)
		}
	}
}

func TestReportTotalsAndDailyBuckets(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	query, err := ParseQuery(url.Values{"timezone": {"Asia/Shanghai"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	rows := []db.UsageAggregate{
		{Day: "2026-10-01", Model: "model-a", Requests: 2, InputTokens: 100, CacheReadTokens: 200, CacheWriteTokens: 300, ModelCents: 12, SearchCents: 3},
		{Day: "2026-10-02", Model: "model-b", Requests: 1, OutputTokens: 50, ModelCents: 5, Incomplete: 1, Unpriced: 1},
	}
	report := BuildReport(rows, query.Data)
	if report.Summary.SubtotalCents != 17 || report.Summary.SearchRequests != 0 || len(report.Daily) != 31 {
		t.Fatalf("汇总或日期桶错误：%+v", report)
	}
	if report.Daily[2].Metrics.Requests != 0 || report.Summary.Incomplete != 1 || report.Summary.Unpriced != 1 {
		t.Fatalf("空日或不完整状态错误：%+v", report)
	}
	if report.Summary.InputTokens != 100 || report.Summary.CacheWriteTokens != 300 {
		t.Fatal("缓存被重复累计到非缓存输入")
	}
	priced := ApplyWebSearchCounts(report, []db.WebSearchDay{{Day: "2026-10-01", Name: "web_search", Searches: 1000}})
	if priced.Summary.SearchRequests != 1000 || priced.Summary.SearchCost != "0.694444" || priced.Summary.SearchCents != 69 || priced.Summary.SubtotalCents != 86 || priced.Daily[0].Metrics.SearchCost != "0.694444" || len(priced.Daily[0].Metrics.ToolCosts) != 1 || priced.Daily[0].Metrics.ToolCosts[0].Name != "web_search" {
		t.Fatalf("搜索费用未按成功调用汇总：%+v", priced.Summary)
	}
	recorded := ApplyWebSearchCounts(report, []db.WebSearchDay{{Day: "2026-10-01", Name: "web_search", CNY: "10", Per: "1000", Exchange: "7.2", Searches: 1000}})
	if recorded.Summary.SearchCost != "1.388889" || recorded.Summary.SearchCents != 139 {
		t.Fatalf("已记录单价被当前单价覆盖：%s %d", recorded.Summary.SearchCost, recorded.Summary.SearchCents)
	}
}

func TestModelCostsSplitByModel(t *testing.T) {
	input := "3.6"
	raw, err := json.Marshal(billing.ModelPrice{ExchangeRate: "7.2", Rates: &billing.Rates{Input: &input}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	query, err := ParseQuery(url.Values{"timezone": {"Asia/Shanghai"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	report := ApplyModelCosts(BuildReport(nil, query.Data), []db.ModelUsage{
		{Day: "2026-10-01", Model: "flash", PriceSnapshot: raw, InputTokens: 1_000_000},
		{Day: "2026-10-01", Model: "pro", PriceSnapshot: raw, InputTokens: 2_000_000},
	})
	if len(report.Summary.ModelCosts) != 2 || report.Summary.ModelCosts[0].Name != "flash" || report.Summary.ModelCosts[0].Cost != "0.500000" || report.Summary.ModelCosts[1].Name != "pro" || report.Summary.ModelCosts[1].Cost != "1.000000" {
		t.Fatalf("模型费用未按模型名拆开：%+v", report.Summary.ModelCosts)
	}
	if report.Daily[0].Metrics.ModelCosts[0].Name != "flash" {
		t.Fatalf("每日模型图例错误：%+v", report.Daily[0].Metrics.ModelCosts)
	}
	stored := ApplyModelCosts(BuildReport(nil, query.Data), []db.ModelUsage{
		{Day: "2026-10-01", Model: "flash", ModelCostUSD: "0.004614", PriceSnapshot: raw, InputTokens: 1_000_000},
	})
	if stored.Summary.ModelCost != "0.004614" {
		t.Fatalf("已保存的 Token 费用被快照重算覆盖：%s", stored.Summary.ModelCost)
	}
	cacheInput := "3"
	cacheOnly := billing.ModelPrice{Rates: &billing.Rates{Input: &cacheInput, Output: &cacheInput}}
	cacheRaw, err := json.Marshal(cacheOnly)
	if err != nil {
		t.Fatal(err)
	}
	fallback := ApplyModelCosts(BuildReport(nil, query.Data), []db.ModelUsage{
		{Day: "2026-10-01", Model: "flash", PriceSnapshot: cacheRaw, CacheReadTokens: 1_000_000},
	})
	if fallback.Summary.ModelCost != "3.000000" {
		t.Fatalf("缺缓存单价时未按预算口径计费：%s", fallback.Summary.ModelCost)
	}
}

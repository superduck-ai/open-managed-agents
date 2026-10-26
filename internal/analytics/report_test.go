package analytics

import (
	"net/url"
	"testing"
	"time"

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
	if report.Summary.SubtotalCents != 20 || len(report.Daily) != 31 {
		t.Fatalf("汇总或日期桶错误：%+v", report)
	}
	if report.Daily[2].Metrics.Requests != 0 || report.Summary.Incomplete != 1 || report.Summary.Unpriced != 1 {
		t.Fatalf("空日或不完整状态错误：%+v", report)
	}
	if report.Summary.InputTokens != 100 || report.Summary.CacheWriteTokens != 300 {
		t.Fatal("缓存被重复累计到非缓存输入")
	}
}

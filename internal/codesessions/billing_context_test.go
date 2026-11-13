package codesessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestRuntimeBilling(t *testing.T) {
	calculator := billing.NewCalculator(map[string]billing.ModelPrice{"narrows/model": {Source: "narrows", Version: "v1", Rates: &billing.Rates{Input: new("7.2"), Output: new("14.4")}}})
	if _, err := makeRuntimeBilling(db.Session{Budget: json.RawMessage(`{}`)}, db.SessionUsageTotals{}, nil, calculator); err == nil {
		t.Fatal("非法预算被接受")
	}
	session := db.Session{Budget: json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"100","currency":"USD"}}`), BudgetReachedAt: new(time.Now())}
	result, err := makeRuntimeBilling(session, db.SessionUsageTotals{ListCostCents: 120}, []string{"narrows/model", "missing"}, calculator)
	if err != nil {
		t.Fatal(err)
	}
	if !result.BudgetReached || result.Remaining.Amount != 0 || result.Usage.Amount != 120 || result.Pricing["missing"] != nil || len(result.Pricing) != 2 {
		t.Fatalf("错误上下文: %+v", result)
	}
	*result.Pricing["narrows/model"].Rates.Input = "999"
	if *calculator.PriceSnapshot("narrows/model").Rates.Input != "7.2" {
		t.Fatal("上下文修改影响请求快照")
	}
	result, err = makeRuntimeBilling(db.Session{}, db.SessionUsageTotals{}, nil, calculator)
	if err != nil || result.Budget != nil || result.Remaining != nil {
		t.Fatalf("空预算: %+v, %v", result, err)
	}
}

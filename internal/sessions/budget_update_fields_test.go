package sessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestBudgetOnlyUpdateProducesChangedField(t *testing.T) {
	before := db.Session{Budget: json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"100","currency":"USD"}}`)}
	after := before
	after.Budget = json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"200","currency":"USD"}}`)
	if _, ok := changedSessionFields(before, after)["budget"]; !ok {
		t.Fatal("预算修改未参与字段变化检测")
	}
	after.Budget = nil
	if value, ok := changedSessionFields(before, after)["budget"]; !ok || value != nil {
		t.Fatalf("预算移除必须携带 null：%v", value)
	}
}

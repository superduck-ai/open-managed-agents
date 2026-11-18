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
	if _, ok := changedSessionFields(before, after, false)["budget"]; !ok {
		t.Fatal("预算修改未参与字段变化检测")
	}
	after.Budget = nil
	if value, ok := changedSessionFields(before, after, false)["budget"]; !ok || value != nil {
		t.Fatalf("预算移除必须携带 null：%v", value)
	}
}

func TestTitleUpdateFieldsOnlyIncludeExplicitChanges(t *testing.T) {
	before := db.Session{}
	after := db.Session{Title: new("Generated title")}
	if _, ok := changedSessionFields(before, after, false)["title"]; ok {
		t.Fatal("unrequested automatic title leaked into session.updated")
	}
	if title, ok := changedSessionFields(before, after, true)["title"]; !ok || title != after.Title {
		t.Fatal("explicit title update missing")
	}
	if _, ok := changedSessionFields(after, after, true)["title"]; ok {
		t.Fatal("same-text manual pin generated a public change")
	}
}

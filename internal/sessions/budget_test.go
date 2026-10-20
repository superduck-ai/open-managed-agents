package sessions

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionBudgetUnpricedValidation(t *testing.T) {
	model := json.RawMessage(`{"id":"real-model-without-configured-list-price"}`)
	if err := rejectUnpricedSessionBudget(nil, model); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`null`, `[]`, `{"type":"limit","max_list_cost":{"amount":"0","currency":"USD"}}`, `{"type":"limit","max_list_cost":{"amount":"01","currency":"USD"}}`, `{"type":"limit","max_list_cost":{"amount":"1.0","currency":"USD"}}`, `{"type":"limit","max_list_cost":{"amount":"1","currency":"EUR"}}`, `{"type":"limit","max_list_cost":{"amount":1,"currency":"USD"}}`} {
		if err := rejectUnpricedSessionBudget(json.RawMessage(body), model); err == nil || strings.Contains(err.Error(), "model_not_budgetable") {
			t.Fatalf("invalid shape must fail before pricing: %s %v", body, err)
		}
	}
	err := rejectUnpricedSessionBudget(json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"1","currency":"USD"}}`), model)
	if err == nil || !strings.Contains(err.Error(), "model_not_budgetable") {
		t.Fatalf("valid budget without pricing must fail closed: %v", err)
	}
}

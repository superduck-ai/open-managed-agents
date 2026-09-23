package sessions

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestParseBudgetInput(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantBudget  bool
		wantRemoved bool
		wantErr     bool
	}{
		{name: "absent", raw: "", wantBudget: false, wantRemoved: false},
		{name: "null removes", raw: "null", wantBudget: false, wantRemoved: true},
		{name: "valid", raw: `{"type":"limit","max_list_cost":{"amount":"2500","currency":"USD"}}`, wantBudget: true},
		{name: "invalid", raw: `{"type":"limit","max_list_cost":{"amount":"0"}}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			budget, removed, err := ParseBudgetInput(json.RawMessage(test.raw))
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseBudgetInput(%s) = no error, want error", test.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBudgetInput(%s) error = %v", test.raw, err)
			}
			if (budget != nil) != test.wantBudget {
				t.Fatalf("budget present = %v, want %v", budget != nil, test.wantBudget)
			}
			if removed != test.wantRemoved {
				t.Fatalf("removed = %v, want %v", removed, test.wantRemoved)
			}
		})
	}
}

func TestBudgetGate(t *testing.T) {
	for _, eventType := range []string{"user.tool_confirmation", "user.tool_result", "user.custom_tool_result", "user.interrupt"} {
		if err := budgetGate(eventType); err != nil {
			t.Fatalf("budgetGate(%s) = %v, want nil", eventType, err)
		}
	}
	if err := budgetGate("user.message"); err == nil {
		t.Fatal("budgetGate(user.message) = nil, want error")
	}
}

func TestFilterEventsAtBudgetCap(t *testing.T) {
	inputs := []json.RawMessage{
		json.RawMessage(`{"type":"user.tool_result","tool_use_id":"tool_1"}`),
		json.RawMessage(`{"type":"user.interrupt"}`),
		json.RawMessage(`{"type":"user.custom_tool_result"}`),
	}
	filtered, err := filterEventsAtBudgetCap(inputs)
	if err != nil {
		t.Fatalf("filterEventsAtBudgetCap error = %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2 (interrupt dropped)", len(filtered))
	}

	if _, err := filterEventsAtBudgetCap([]json.RawMessage{json.RawMessage(`{"type":"user.message"}`)}); err == nil {
		t.Fatal("non-settlement event accepted, want budget gate error")
	}
	if _, err := filterEventsAtBudgetCap([]json.RawMessage{json.RawMessage(`"not-an-object"`)}); err == nil {
		t.Fatal("non-object event accepted, want error")
	}
}

func TestSessionUsageJSON(t *testing.T) {
	totals := db.SessionUsageTotals{ListCostCents: 2500, InputTokens: 100, OutputTokens: 50}
	usage := sessionUsageJSON(totals, json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"2500","currency":"USD"}}`))
	var parsed map[string]any
	if err := json.Unmarshal(usage, &parsed); err != nil {
		t.Fatalf("usage json invalid: %v", err)
	}
	listCost, ok := parsed["list_cost"].(map[string]any)
	if !ok || listCost["amount"] != "2500" || listCost["currency"] != "USD" {
		t.Fatalf("list_cost = %v, want amount 2500 USD", parsed["list_cost"])
	}
	budget, ok := parsed["budget"].(map[string]any)
	if !ok || budget["type"] != "limit" {
		t.Fatalf("budget = %v, want echoed budget object", parsed["budget"])
	}

	usageNoBudget := sessionUsageJSON(totals, nil)
	var parsedNoBudget map[string]any
	if err := json.Unmarshal(usageNoBudget, &parsedNoBudget); err != nil {
		t.Fatalf("usage json invalid: %v", err)
	}
	budgetValue, has := parsedNoBudget["budget"]
	if !has || budgetValue != nil {
		t.Fatalf("budget = %v (present=%v), want explicit null without a configured budget", budgetValue, has)
	}
	if parsedNoBudget["list_cost"].(map[string]any)["amount"] != "2500" {
		t.Fatalf("list_cost.amount = %v", parsedNoBudget["list_cost"])
	}
}

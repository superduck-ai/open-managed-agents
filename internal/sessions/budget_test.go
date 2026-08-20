package sessions

import (
	"encoding/json"
	"testing"
)

func TestNormalizeBudgetRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"decimal amount", `{"type":"limit","max_list_cost":{"amount":"25.00","currency":"USD"}}`},
		{"zero amount", `{"type":"limit","max_list_cost":{"amount":"0","currency":"USD"}}`},
		{"negative amount", `{"type":"limit","max_list_cost":{"amount":"-5","currency":"USD"}}`},
		{"leading zero", `{"type":"limit","max_list_cost":{"amount":"0125","currency":"USD"}}`},
		{"non-numeric", `{"type":"limit","max_list_cost":{"amount":"abc","currency":"USD"}}`},
		{"non-USD currency", `{"type":"limit","max_list_cost":{"amount":"125","currency":"EUR"}}`},
		{"wrong type", `{"type":"soft","max_list_cost":{"amount":"125","currency":"USD"}}`},
		{"missing max_list_cost", `{"type":"limit"}`},
		{"not object", `"limit"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeBudget(json.RawMessage(tc.raw)); err == nil {
				t.Fatalf("normalizeBudget(%s) 应报错", tc.raw)
			}
		})
	}
}

func TestNormalizeBudgetValid(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"basic", `{"type":"limit","max_list_cost":{"amount":"125","currency":"USD"}}`, `{"max_list_cost":{"amount":"125","currency":"USD"},"type":"limit"}`},
		{"single cent", `{"type":"limit","max_list_cost":{"amount":"50","currency":"USD"}}`, `{"max_list_cost":{"amount":"50","currency":"USD"},"type":"limit"}`},
		{"trims amount", `{"type":"limit","max_list_cost":{"amount":" 125 ","currency":"USD"}}`, `{"max_list_cost":{"amount":"125","currency":"USD"},"type":"limit"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeBudget(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("normalizeBudget(%s) 应成功: %v", tc.raw, err)
			}
			if string(got) != tc.want {
				t.Fatalf("normalizeBudget(%s) = %s, want %s", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeBudgetNullAndOmitted(t *testing.T) {
	if got, err := normalizeBudget(json.RawMessage(`null`)); err != nil || got != nil {
		t.Fatalf("normalizeBudget(null) = %v, %v, want nil", got, err)
	}
	if got, err := normalizeBudget(nil); err != nil || got != nil {
		t.Fatalf("normalizeBudget(nil) = %v, %v, want nil", got, err)
	}
}

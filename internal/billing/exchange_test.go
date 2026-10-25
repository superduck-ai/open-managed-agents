package billing

import "testing"

func TestExchangeBeforeRounding(t *testing.T) {
	r := Rates{Input: new("3.6")}
	for _, rate := range []string{"0", "-1", "invalid"} {
		if _, ok := r.cost(TokenUsage{InputTokens: 1000000}, rate); ok {
			t.Fatal("接受无效汇率")
		}
	}
	for _, tc := range []struct{ tokens, cents int64 }{{9999, 0}, {10000, 1}, {10001, 1}, {1000000, 50}} {
		got, ok := r.cost(TokenUsage{InputTokens: tc.tokens}, "7.2")
		if !ok || got != tc.cents {
			t.Fatalf("%d: %d/%v", tc.tokens, got, ok)
		}
	}
}

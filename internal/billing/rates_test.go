package billing

import "testing"

func TestRatesTTLAndUnknown(t *testing.T) {
	rates := Rates{Input: new("3"), Output: new("15"), CacheRead: new("0.3"), CacheWrite5m: new("3.75"), CacheWrite1h: new("6")}
	for _, u := range []TokenUsage{{CacheCreationInputTokens: 1}, {CacheCreationInputTokens: 1, CacheWrite5mTokens: 2}, {InputTokens: -1}} {
		if _, ok := rates.Cost(u); ok {
			t.Fatal("不完整或非法用量不应计价")
		}
	}
	u := TokenUsage{InputTokens: 1000000, OutputTokens: 1000000, CacheReadInputTokens: 1000000, CacheCreationInputTokens: 2000000, CacheWrite5mTokens: 1000000, CacheWrite1hTokens: 1000000}
	cents, ok := rates.Cost(u)
	if !ok || cents != 2805 {
		t.Fatalf("费用=%d，期望2805美分", cents)
	}
	rates.CacheWrite = new("0")
	if cents, ok := rates.Cost(TokenUsage{CacheCreationInputTokens: 1000000}); !ok || cents != 0 {
		t.Fatal("明确免费应该可计价")
	}
}

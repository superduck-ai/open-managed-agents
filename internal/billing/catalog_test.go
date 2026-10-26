package billing

import (
	"testing"
	"time"
)

func TestCatalogAliases(t *testing.T) {
	aliases := PriceAliases(map[string]ModelPrice{"a/same": {SourceModel: "a/same"}, "b/same": {SourceModel: "b/same"}, "a/unique": {SourceModel: "a/unique"}, "~a/unique": {SourceModel: "~a/unique"}})
	if _, ok := aliases["same"]; ok {
		t.Fatal("歧义短名称被匹配")
	}
	if aliases["unique"].SourceModel != "a/unique" || aliases["b/same"].SourceModel != "b/same" {
		t.Fatal("模型匹配错误")
	}
}
func TestPriceOverrideBoundaries(t *testing.T) {
	p := ModelPrice{Rates: &Rates{Input: new("1"), Output: new("2")}, Overrides: []PriceOverride{{MinPromptTokens: new(int64(100)), Rates: Rates{Input: new("3")}}, {UTCStart: new(1600), UTCEnd: new(0), UTCDays: []string{"wednesday"}, Rates: Rates{Output: new("4")}}}}
	for _, tc := range []struct {
		hour          int
		tokens        int64
		input, output string
	}{{15, 100, "1", "2"}, {16, 101, "3", "4"}, {0, 101, "3", "2"}} {
		p.PricedAt = time.Date(2026, 10, 14, tc.hour, 0, 0, 0, time.UTC)
		r := p.effectiveRates(TokenUsage{CacheReadInputTokens: tc.tokens})
		if *r.Input != tc.input || *r.Output != tc.output {
			t.Fatalf("边界错误 %+v", tc)
		}
	}
}

package codesessions

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
)

func TestModelRequestEventUsageEncoding(t *testing.T) {
	for _, test := range []struct {
		name  string
		usage *ModelRequestUsage
		want  string
	}{
		{name: "absent"},
		{name: "unknown", usage: &ModelRequestUsage{}, want: `{}`},
		{name: "explicit zero", usage: &ModelRequestUsage{
			InputTokens: new(int64(0)), OutputTokens: new(int64(0)),
			CacheCreationInputTokens: new(int64(0)), CacheReadInputTokens: new(int64(0)),
		}, want: `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}`},
		{name: "partial", usage: &ModelRequestUsage{OutputTokens: new(int64(7))}, want: `{"output_tokens":7}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := jsonv2.Marshal(modelRequestEvent{Usage: test.usage})
			if err != nil {
				t.Fatal(err)
			}
			var event map[string]json.RawMessage
			if err := jsonv2.Unmarshal(encoded, &event); err != nil {
				t.Fatal(err)
			}
			if got := string(event["model_usage"]); got != test.want {
				t.Fatalf("usage = %s, want %s", got, test.want)
			}
		})
	}
}

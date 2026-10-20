package db

import (
	"encoding/json"
	"testing"
)

func TestSessionActivityProjectionPreservesMeasuredTokens(t *testing.T) {
	var p sessionActivityProjection
	if err := p.Scan([]byte(`{"usage":{"input_tokens":19,"active_seconds":10},"stats":{"active_seconds":22}}`)); err != nil {
		t.Fatal(err)
	}
	row := sessionRow{Usage: []byte(`{"input_tokens":19}`), Stats: []byte(`{}`), Activity: p}
	value := row.session()
	var usage struct {
		InputTokens   int     `json:"input_tokens"`
		ActiveSeconds float64 `json:"active_seconds"`
	}
	if err := json.Unmarshal(value.Usage, &usage); err != nil || usage.InputTokens != 19 || usage.ActiveSeconds != 10 {
		t.Fatal("projected usage", usage, err)
	}
	value.Usage[0] = '['
	if string(p.Usage)[0] != '{' {
		t.Fatal("public result aliases row")
	}
	for _, source := range []any{19, []byte(`{`), `{"usage":{}}`} {
		if err := p.Scan(source); err == nil {
			t.Fatal("invalid projection accepted", source)
		}
	}
	if err := p.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if string(p.usage([]byte(`{"output_tokens":3}`))) != `{"output_tokens":3}` {
		t.Fatal("unset manually constructed row loses stored counters")
	}
}

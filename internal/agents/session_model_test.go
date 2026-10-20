package agents

import (
	"encoding/json"
	"testing"
)

func TestSessionModelOverrideReplacesBaseAndKeepsGeo(t *testing.T) {
	raw, err := NormalizeSessionModelOverride(json.RawMessage(`{"id":"fixture","inference_geo":"us"}`))
	if err != nil {
		t.Fatal(err)
	}
	var model map[string]any
	if err := json.Unmarshal(raw, &model); err != nil {
		t.Fatal(err)
	}
	if model["id"] != "fixture" || model["inference_geo"] != "us" || model["speed"] != "standard" {
		t.Fatal(string(raw))
	}
	for _, bad := range []string{`null`, `{}`, `{"id":"fixture","inference_geo":"eu"}`} {
		if _, err := NormalizeSessionModelOverride(json.RawMessage(bad)); err == nil {
			t.Fatal("bad model override accepted", bad)
		}
	}
}

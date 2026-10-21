package agents

import (
	"encoding/json"
	"testing"
)

func TestNormalizeModelRejectsInvalidEffort(t *testing.T) {
	for _, value := range []string{`"none"`, `""`, `"LOW"`, `"invalid"`, `4`, `true`, `[]`, `{}`, `{"type":false}`, `{"type":"unsupported"}`} {
		if _, err := normalizeModel(json.RawMessage(`{"id":"deepseek-flash","effort":` + value + `}`)); err == nil {
			t.Fatalf("invalid effort accepted: %s", value)
		}
	}
}

func TestNormalizeModelPreservesNativeEffortJSON(t *testing.T) {
	for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
		for _, value := range []string{`"` + level + `"`, `{"type":"` + level + `"}`} {
			raw := json.RawMessage(`{"id":"deepseek-flash","effort":` + value + `,"inference_geo":"us"}`)
			model, err := normalizeModel(raw)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(model)
			if err != nil {
				t.Fatal(err)
			}
			var stored struct {
				ID     string `json:"id"`
				Speed  string `json:"speed"`
				Effort struct {
					Type string `json:"type"`
				} `json:"effort"`
				Geo string `json:"inference_geo"`
			}
			if err = json.Unmarshal(encoded, &stored); err != nil {
				t.Fatal(err)
			}
			if stored.Effort.Type != level || stored.ID != "deepseek-flash" || stored.Speed != "standard" || stored.Geo != "us" {
				t.Fatalf("model field lost: %s", encoded)
			}
			override, err := NormalizeSessionModelOverride(raw, identityModelID)
			if err != nil || string(override) != string(encoded) {
				t.Fatalf("override normalization differs: %s %v", override, err)
			}
		}
	}
}

func TestNormalizeModelOmittedOrNullEffortUsesDefault(t *testing.T) {
	for _, raw := range []string{`{"id":"deepseek-flash"}`, `{"id":"deepseek-flash","effort":null}`} {
		model, err := normalizeModel(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(model)
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["effort"]; ok {
			t.Fatalf("default effort must stay omitted: %s", encoded)
		}
	}
}

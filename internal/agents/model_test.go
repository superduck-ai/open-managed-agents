package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeModelRejectsInvalidObjectFields(t *testing.T) {
	testCases := []struct {
		name      string
		raw       string
		wantError string
	}{
		{
			name:      "missing id",
			raw:       `{"speed":"standard"}`,
			wantError: "model.id is required",
		},
		{
			name:      "empty id",
			raw:       `{"id":""}`,
			wantError: "model.id must be a non-empty string",
		},
		{
			name:      "invalid speed",
			raw:       `{"id":"claude-sonnet-4-6","speed":"slow"}`,
			wantError: "model.speed must be standard or fast",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := normalizeModel(json.RawMessage(testCase.raw))
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("normalizeModel() error = %v, want %q", err, testCase.wantError)
			}
		})
	}
}

func TestNormalizeModelPreservesRealID(t *testing.T) {
	testCases := []struct {
		name string
		raw  string
		want normalizedAgentModel
	}{
		{
			name: "string model uses standard speed",
			raw:  `"kimi-k2.5"`,
			want: normalizedAgentModel{ID: "kimi-k2.5", Speed: "standard"},
		},
		{
			name: "object model preserves fast speed",
			raw:  `{"id":"kimi-k2.5","speed":"fast"}`,
			want: normalizedAgentModel{ID: "kimi-k2.5", Speed: "fast"},
		},
		{
			name: "string model keeps surrounding whitespace",
			raw:  `" kimi-k2.5 "`,
			want: normalizedAgentModel{ID: " kimi-k2.5 ", Speed: "standard"},
		},
		{
			name: "object model keeps surrounding whitespace",
			raw:  `{"id":" kimi-k2.5 ","speed":"fast"}`,
			want: normalizedAgentModel{ID: " kimi-k2.5 ", Speed: "fast"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := normalizeModel(json.RawMessage(testCase.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got != testCase.want {
				t.Fatalf("normalizeModel() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

func TestNormalizeModelPreservesInferenceGeoInStoredJSON(t *testing.T) {
	for _, geo := range []string{"us", "global"} {
		raw := json.RawMessage(`{"id":"deepseek-flash","inference_geo":"` + geo + `"}`)
		model, err := normalizeModel(raw)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(model)
		if err != nil {
			t.Fatal(err)
		}
		var stored map[string]any
		if err := json.Unmarshal(encoded, &stored); err != nil {
			t.Fatal(err)
		}
		if stored["inference_geo"] != geo || stored["id"] != "deepseek-flash" || stored["speed"] != "standard" {
			t.Fatalf("model pin lost before storage: %s", encoded)
		}
	}
	for _, raw := range []string{`"deepseek-flash"`, `{"id":"deepseek-flash"}`, `{"id":"deepseek-flash","inference_geo":null}`} {
		model, err := normalizeModel(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(model)
		if err != nil {
			t.Fatal(err)
		}
		var stored map[string]any
		if err := json.Unmarshal(encoded, &stored); err != nil {
			t.Fatal(err)
		}
		if _, exists := stored["inference_geo"]; exists {
			t.Fatalf("unset geography became a pin: %s", encoded)
		}
	}
	for _, value := range []string{`"eu"`, `""`, `" us "`, `42`, `true`} {
		if _, err := normalizeModel(json.RawMessage(`{"id":"deepseek-flash","inference_geo":` + value + `}`)); err == nil {
			t.Fatalf("invalid pin accepted: %s", value)
		}
	}
}

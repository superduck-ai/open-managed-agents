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

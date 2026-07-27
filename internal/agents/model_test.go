package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
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
			raw:       `{"id":" "}`,
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
			_, err := normalizeModel(json.RawMessage(testCase.raw), nil)
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("normalizeModel() error = %v, want %q", err, testCase.wantError)
			}
		})
	}
}

func TestNormalizeModelUsesMappedID(t *testing.T) {
	mappings := map[string]string{"claude-sonnet-4-6": "glm-5-turbo"}
	testCases := []struct {
		name string
		raw  string
		want normalizedAgentModel
	}{
		{
			name: "string model uses standard speed",
			raw:  `"claude-sonnet-4-6"`,
			want: normalizedAgentModel{ID: "glm-5-turbo", Speed: "standard"},
		},
		{
			name: "object model preserves fast speed",
			raw:  `{"id":"claude-sonnet-4-6","speed":"fast"}`,
			want: normalizedAgentModel{ID: "glm-5-turbo", Speed: "fast"},
		},
		{
			name: "string model trims surrounding whitespace",
			raw:  `" claude-sonnet-4-6 "`,
			want: normalizedAgentModel{ID: "glm-5-turbo", Speed: "standard"},
		},
		{
			name: "object model trims surrounding whitespace",
			raw:  `{"id":" claude-sonnet-4-6 ","speed":"fast"}`,
			want: normalizedAgentModel{ID: "glm-5-turbo", Speed: "fast"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := normalizeModel(json.RawMessage(testCase.raw), mappings)
			if err != nil {
				t.Fatal(err)
			}
			if got != testCase.want {
				t.Fatalf("normalizeModel() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

func TestStateFromUpdateMapsInheritedModel(t *testing.T) {
	handler := Handler{
		cfg: config.Config{
			AnthropicUpstream: config.AnthropicUpstreamConfig{
				ModelMappings: map[string]string{"claude-sonnet-4-6": "glm-5-turbo"},
			},
		},
	}
	state, err := handler.stateFromUpdate(
		nil,
		auth.Principal{},
		db.Agent{
			Model: json.RawMessage(`{"id":"claude-sonnet-4-6","speed":"fast"}`),
		},
		map[string]json.RawMessage{
			"description": json.RawMessage(`"updated without model"`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	var got normalizedAgentModel
	if err := json.Unmarshal(state.Model, &got); err != nil {
		t.Fatal(err)
	}
	want := normalizedAgentModel{ID: "glm-5-turbo", Speed: "fast"}
	if got != want {
		t.Fatalf("stateFromUpdate() model = %#v, want %#v", got, want)
	}
}

package agents

import (
	"encoding/json"
	"reflect"
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
		{
			name:      "invalid effort string",
			raw:       `{"id":"claude-sonnet-4-6","effort":"extreme"}`,
			wantError: "model.effort must be low, medium, high, xhigh, or max",
		},
		{
			name:      "invalid effort object",
			raw:       `{"id":"claude-sonnet-4-6","effort":{"type":"extreme"}}`,
			wantError: "model.effort.type must be low, medium, high, xhigh, or max",
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
			raw:  `{"id":"claude-sonnet-4-6","speed":"fast","effort":"high"}`,
			want: normalizedAgentModel{ID: "glm-5-turbo", Speed: "fast", Effort: &agentModelEffort{Type: "high"}},
		},
		{
			name: "object model normalizes effort object",
			raw:  `{"id":"claude-sonnet-4-6","effort":{"type":"xhigh"}}`,
			want: normalizedAgentModel{ID: "glm-5-turbo", Speed: "standard", Effort: &agentModelEffort{Type: "xhigh"}},
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
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("normalizeModel() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

func TestStateFromUpdateAppliesManagedAgentEffortSemantics(t *testing.T) {
	t.Parallel()
	handler := Handler{catalog: agentTestCatalog{models: []string{"model-1", "model-2"}}}
	current := db.Agent{Model: json.RawMessage(`{"id":"model-1","speed":"fast","effort":{"type":"high"}}`)}

	preserved, err := handler.stateFromUpdate(agentCatalogRequest(`{}`), auth.Principal{}, current, map[string]json.RawMessage{
		"model": json.RawMessage(`{"id":"model-1","speed":"standard"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var preservedModel normalizedAgentModel
	if err := json.Unmarshal(preserved.Model, &preservedModel); err != nil {
		t.Fatal(err)
	}
	if preservedModel.Effort == nil || preservedModel.Effort.Type != "high" {
		t.Fatalf("same-model effort = %#v, want preserved high", preservedModel.Effort)
	}

	changed, err := handler.stateFromUpdate(agentCatalogRequest(`{}`), auth.Principal{}, current, map[string]json.RawMessage{
		"model": json.RawMessage(`{"id":"model-2","speed":"standard"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var changedModel normalizedAgentModel
	if err := json.Unmarshal(changed.Model, &changedModel); err != nil {
		t.Fatal(err)
	}
	if changedModel.Effort != nil {
		t.Fatalf("changed-model effort = %#v, want provider default", changedModel.Effort)
	}

	replaced, err := handler.stateFromUpdate(agentCatalogRequest(`{}`), auth.Principal{}, current, map[string]json.RawMessage{
		"model": json.RawMessage(`"model-1"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var replacedModel normalizedAgentModel
	if err := json.Unmarshal(replaced.Model, &replacedModel); err != nil {
		t.Fatal(err)
	}
	if replacedModel.Effort != nil {
		t.Fatalf("string-model effort = %#v, want provider default", replacedModel.Effort)
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

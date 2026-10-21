package agents

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCMARosterGeographyUsesPinnedVersionsAndRejectsMixedPins(t *testing.T) {
	for _, setting := range []string{"subagents", "workflows"} {
		for _, pair := range []struct {
			parent, child string
			reject        bool
		}{{"", "", false}, {"us", "us", false}, {"global", "global", false}, {"us", "", true}, {"", "us", true}, {"us", "global", true}, {"global", "us", true}} {
			model := func(geo string) json.RawMessage {
				if geo == "" {
					return json.RawMessage(`{"id":"fixture"}`)
				}
				return json.RawMessage(`{"id":"fixture","inference_geo":"` + geo + `"}`)
			}
			multi := json.RawMessage(`{"type":"multiagent_20261001","` + setting + `":{"type":"enabled","predefined_agents":[{"type":"agent","id":"child","version":3}]}}`)
			calls := 0
			lookup := func(ref agentReference) (json.RawMessage, error) {
				calls++
				if ref.ID != "child" || ref.Version != 3 {
					t.Fatal("not fixed child version", ref)
				}
				return model(pair.child), nil
			}
			err := validateCMARosterGeography(model(pair.parent), multi, "parent", 1, lookup)
			if errors.Is(err, errRosterInferenceGeoMismatch) != pair.reject || calls != 1 {
				t.Fatalf("%s parent=%q child=%q err=%v calls=%d", setting, pair.parent, pair.child, err, calls)
			}
		}
	}
}

func TestCMARosterGeographySelfHistoryAndUnchangedUpdate(t *testing.T) {
	model := json.RawMessage(`{"id":"fixture","inference_geo":"us"}`)
	multi := json.RawMessage(`{"type":"multiagent_20261001","subagents":{"type":"enabled","predefined_agents":[{"type":"agent","id":"parent","version":2}]}}`)
	calls := 0
	lookup := func(ref agentReference) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"id":"fixture"}`), nil
	}
	if err := validateCMARosterGeography(model, multi, "parent", 2, lookup); err != nil || calls != 0 {
		t.Fatal("current self reloaded", err, calls)
	}
	if err := validateCMARosterGeography(model, multi, "parent", 3, lookup); !errors.Is(err, errRosterInferenceGeoMismatch) || calls != 1 {
		t.Fatal("historical self ignored", err, calls)
	}
	h := &Handler{}
	current := db.Agent{ExternalID: "parent", CurrentVersion: 1, Model: json.RawMessage(`{"id":"fixture"}`), Multiagent: json.RawMessage(`{"type":"multiagent_20261001","subagents":{"type":"enabled","predefined_agents":[{"type":"agent","id":"parent","version":2}]}}`)}
	state, err := h.stateFromUpdate(httptest.NewRequest("POST", "/", nil), auth.Principal{}, current, &agentMutationRequest{})
	if err != nil || len(state.Multiagent) == 0 {
		t.Fatal("unchanged state/current self rejected", state, err)
	}
}

package agents

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestResolvedRosterReferenceRejectsEveryConfiguredMultiagent(t *testing.T) {
	for _, raw := range []string{
		`{"type":"coordinator","agents":[{"type":"self"}]}`,
		`{"type":"multiagent_20261001"}`,
		`{"type":"multiagent_20261001","subagents":{"type":"disabled"},"workflows":{"type":"disabled"},"advisor":{"type":"disabled"}}`,
	} {
		for _, version := range []int{1, 5} {
			record := db.Agent{ExternalID: "agent_nested", CurrentVersion: version, Multiagent: json.RawMessage(raw)}
			ref, self, err := resolvedRosterReference(record, "agent_parent")
			if !errors.Is(err, errNestedMultiagentReference) || self || ref.ID != "" {
				t.Fatalf("accepted nested agent at version %d: %s, ref=%+v self=%v err=%v", version, raw, ref, self, err)
			}
		}
	}
}

func TestResolvedRosterReferenceKeepsPlainAgentPinned(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`)} {
		for _, version := range []int{1, 5} {
			record := db.Agent{ExternalID: "agent_worker", CurrentVersion: version, Multiagent: raw}
			ref, self, err := resolvedRosterReference(record, "agent_parent")
			if err != nil || self || ref.ID != record.ExternalID || ref.Type != "agent" || ref.Version != version {
				t.Fatalf("plain agent changed: ref=%+v self=%v err=%v", ref, self, err)
			}
		}
	}
}

func TestResolvedRosterReferenceKeepsHistoricalSelfPinned(t *testing.T) {
	record := db.Agent{ExternalID: "agent_parent", CurrentVersion: 5, Multiagent: json.RawMessage(`{"type":"multiagent_20261001"}`)}
	ref, self, err := resolvedRosterReference(record, "agent_parent")
	if err != nil || self || ref.ID != record.ExternalID || ref.Version != 5 {
		t.Fatalf("historical self changed: ref=%+v self=%v err=%v", ref, self, err)
	}
}

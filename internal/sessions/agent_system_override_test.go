package sessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"reflect"
	"strings"
	"testing"
)

func TestSessionSystemOverrideReferenceAcceptsPublicField(t *testing.T) {
	var ref sessionAgentReference
	if err := json.Unmarshal([]byte(`{"type":"agent_with_overrides","id":"agent_fixture","version":3,"system":"Session system"}`), &ref); err != nil {
		t.Fatal(err)
	}
	if err := ref.validateType(); err != nil {
		t.Fatalf("public system override rejected: %v", err)
	}
}

func TestSessionSystemOverrideRejectsInvalidTypesAndLimit(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`true`), json.RawMessage(`3`), json.RawMessage(`{}`), json.RawMessage(`[]`), json.RawMessage(`"` + strings.Repeat("a", 100001) + `"`)} {
		if _, err := applySessionSystemOverride(db.Agent{}, raw); err == nil {
			t.Fatalf("invalid system accepted: %.30s", raw)
		}
	}
}

func TestSessionSystemOverrideUsesCopyAndRetainsOtherConfiguration(t *testing.T) {
	original := "source system"
	base := db.Agent{ExternalID: "agent_fixture", CurrentVersion: 3, System: &original, Model: json.RawMessage(`{"id":"fixture","effort":{"type":"high"}}`)}
	for _, tc := range []struct {
		raw  json.RawMessage
		want *string
	}{
		{nil, &original}, {json.RawMessage(`null`), nil}, {json.RawMessage(`""`), new("")}, {json.RawMessage(`"Session system"`), new("Session system")},
	} {
		got, err := applySessionSystemOverride(base, tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got.ExternalID != base.ExternalID || got.CurrentVersion != base.CurrentVersion || string(got.Model) != string(base.Model) || !reflect.DeepEqual(got.System, tc.want) {
			t.Fatalf("unexpected effective configuration: %#v", got)
		}
		if base.System == nil || *base.System != "source system" {
			t.Fatal("source mutated")
		}
		if len(tc.raw) > 0 && got.System != nil && got.System == base.System {
			t.Fatal("override aliases source pointer")
		}
	}
	unicode := strings.Repeat("中", 100000)
	raw, _ := json.Marshal(unicode)
	if _, err := applySessionSystemOverride(base, raw); err != nil {
		t.Fatal("character limit incorrectly uses byte length", err)
	}
}

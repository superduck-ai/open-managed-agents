package sessions

import (
	"encoding/json"
	"testing"
)

func TestSessionModelOverrideReferenceRecognizesPublicType(t *testing.T) {
	for _, kind := range []string{"", "agent", "agent_with_overrides"} {
		raw := json.RawMessage(`{"type":"` + kind + `","id":"agent_fixture","version":3,"model":{"id":"fixture","inference_geo":"us"}}`)
		var ref sessionAgentReference
		if err := json.Unmarshal(raw, &ref); err != nil {
			t.Fatal(err)
		}
		if err := ref.validateType(); err != nil {
			t.Fatal(kind, err)
		}
		if ref.ID != "agent_fixture" || *ref.Version != 3 || len(ref.Model) == 0 {
			t.Fatal(ref)
		}
	}
	if (sessionAgentReference{Type: "future"}).validateType() == nil {
		t.Fatal("unknown type accepted")
	}
	if err := (sessionAgentReference{Type: "agent_with_overrides", Skills: json.RawMessage(`[]`)}).validateType(); err != nil {
		t.Fatal("implemented public skills field rejected", err)
	}
}

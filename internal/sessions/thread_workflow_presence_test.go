package sessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestOrdinaryThreadResponseIncludesNullWorkflowRunID(t *testing.T) {
	for _, parent := range []*string{nil, new("primary")} {
		raw, err := json.Marshal(responseFromThread(db.SessionThread{ExternalID: "thread", ParentThreadExternalID: parent}))
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			t.Fatal("thread response JSON")
		}
		value, present := fields["workflow_run_id"]
		if !present || string(value) != "null" {
			t.Fatalf("ordinary thread workflow reference must be explicit null: %s", raw)
		}
	}
}

package deploymentjobs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
)

func TestStampScheduledDeploymentOccurrenceRequiresScheduledAt(t *testing.T) {
	encoded, err := jsonx.Encode(Args{})
	if err != nil {
		t.Fatalf("encode args: %v", err)
	}
	if err := stampScheduledDeploymentOccurrence(t.Context(), &rivertype.JobInsertParams{EncodedArgs: encoded}); err == nil {
		t.Fatal("stampScheduledDeploymentOccurrence() error = nil")
	}
}

func TestStampScheduledDeploymentOccurrenceCopiesCronTime(t *testing.T) {
	occurrence := time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)
	encoded, err := jsonx.Encode(Args{WorkspaceUUID: "ws", DeploymentExternalID: "depl_1"})
	if err != nil {
		t.Fatalf("encode args: %v", err)
	}
	params := &rivertype.JobInsertParams{EncodedArgs: encoded, ScheduledAt: &occurrence}
	if err := stampScheduledDeploymentOccurrence(t.Context(), params); err != nil {
		t.Fatalf("stampScheduledDeploymentOccurrence() error = %v", err)
	}
	stamped, err := jsonx.Decode[Args](json.RawMessage(params.EncodedArgs))
	if err != nil {
		t.Fatalf("decode stamped args: %v", err)
	}
	if !stamped.ScheduledAt.Equal(occurrence) {
		t.Fatalf("ScheduledAt = %v, want %v", stamped.ScheduledAt, occurrence)
	}
}

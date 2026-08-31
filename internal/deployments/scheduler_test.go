package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestStampScheduledDeploymentOccurrenceCopiesCronTime(t *testing.T) {
	occurrence := time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)
	encoded, err := jsonx.Encode(scheduledDeploymentArgs{WorkspaceUUID: "ws", DeploymentExternalID: "depl_1"})
	if err != nil {
		t.Fatalf("encode args: %v", err)
	}
	params := &rivertype.JobInsertParams{EncodedArgs: encoded, ScheduledAt: &occurrence}
	if err := stampScheduledDeploymentOccurrence(context.Background(), params); err != nil {
		t.Fatalf("stampScheduledDeploymentOccurrence() error = %v", err)
	}
	stamped, err := jsonx.Decode[scheduledDeploymentArgs](json.RawMessage(params.EncodedArgs))
	if err != nil {
		t.Fatalf("decode stamped args: %v", err)
	}
	if !stamped.ScheduledAt.Equal(occurrence) {
		t.Fatalf("ScheduledAt = %v, want %v", stamped.ScheduledAt, occurrence)
	}
}

func TestOccurrenceTimeUsesArgsNotRiverJobScheduledAt(t *testing.T) {
	occurrence := time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)
	got, err := occurrenceTime(&river.Job[scheduledDeploymentArgs]{
		JobRow: &rivertype.JobRow{ScheduledAt: occurrence.Add(time.Second)},
		Args:   scheduledDeploymentArgs{ScheduledAt: occurrence},
	})
	if err != nil || !got.Equal(occurrence) {
		t.Fatalf("occurrenceTime() = (%v, %v), want %v", got, err, occurrence)
	}
}

func TestOccurrenceTimeRejectsMissingArgsScheduledAt(t *testing.T) {
	_, err := occurrenceTime(&river.Job[scheduledDeploymentArgs]{
		JobRow: &rivertype.JobRow{ScheduledAt: time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)},
	})
	if err == nil {
		t.Fatal("occurrenceTime() error = nil")
	}
}

func TestStampScheduledDeploymentOccurrenceRequiresScheduledAt(t *testing.T) {
	encoded, err := jsonx.Encode(scheduledDeploymentArgs{})
	if err != nil {
		t.Fatalf("encode args: %v", err)
	}
	if err := stampScheduledDeploymentOccurrence(context.Background(), &rivertype.JobInsertParams{EncodedArgs: encoded}); err == nil {
		t.Fatal("stampScheduledDeploymentOccurrence() error = nil")
	}
}

func TestClassifyReferenceFailureRetriesInfrastructureErrors(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	failure, err := classifyReferenceFailure("agent", databaseErr, false)
	if !errors.Is(err, databaseErr) || failure != nil {
		t.Fatalf("classifyReferenceFailure() = (%v, %v), want (nil, database error)", failure, err)
	}

	failure, err = classifyReferenceFailure("agent", db.ErrNotFound, false)
	if err != nil || failure == nil || failure.Type != "agent_archived_error" || failure.Message != "Agent not found" {
		t.Fatalf("classifyReferenceFailure(not found) = (%v, %v)", failure, err)
	}
}

func TestShouldAutoPauseUsesOfficialAllowlist(t *testing.T) {
	autoPause := []string{
		"environment_archived_error",
		"agent_archived_error",
		"environment_not_found_error",
		"vault_not_found_error",
		"file_not_found_error",
		"session_resource_not_found_error",
		"workspace_archived_error",
		"organization_disabled_error",
		"memory_store_archived_error",
		"skill_not_found_error",
		"vault_archived_error",
		"unknown_error",
		"self_hosted_resources_unsupported_error",
		"mcp_egress_blocked_error",
	}
	for _, errorType := range autoPause {
		if !shouldAutoPause(runError(errorType, "test")) {
			t.Errorf("shouldAutoPause(%q) = false", errorType)
		}
	}
	for _, errorType := range []string{"session_rate_limited_error", "session_creation_rejected_error", "future_error"} {
		if shouldAutoPause(runError(errorType, "test")) {
			t.Errorf("shouldAutoPause(%q) = true", errorType)
		}
	}
}

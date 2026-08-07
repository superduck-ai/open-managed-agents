package deployments

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestClassifyReferenceFailureRetriesInfrastructureErrors(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	failure, err := classifyReferenceFailure("agent", databaseErr, false)
	if !errors.Is(err, databaseErr) || failure != nil {
		t.Fatalf("classifyReferenceFailure() = (%s, %v), want (nil, database error)", failure, err)
	}

	failure, err = classifyReferenceFailure("agent", db.ErrNotFound, false)
	if err != nil || string(failure) != `{"message":"Agent not found","type":"agent_archived_error"}` {
		t.Fatalf("classifyReferenceFailure(not found) = (%s, %v)", failure, err)
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
		if !shouldAutoPause(json.RawMessage(`{"type":"` + errorType + `"}`)) {
			t.Errorf("shouldAutoPause(%q) = false", errorType)
		}
	}
	for _, errorType := range []string{"session_rate_limited_error", "session_creation_rejected_error", "future_error"} {
		if shouldAutoPause(json.RawMessage(`{"type":"` + errorType + `"}`)) {
			t.Errorf("shouldAutoPause(%q) = true", errorType)
		}
	}
	if shouldAutoPause(json.RawMessage(`not-json`)) {
		t.Error("shouldAutoPause(invalid JSON) = true")
	}
}

func TestScheduledFailureWebhookInputsIncludeAutoPause(t *testing.T) {
	inputs := scheduledFailureWebhookInputs("drun_test", "depl_test", true)
	if len(inputs) != 3 {
		t.Fatalf("scheduledFailureWebhookInputs() length = %d, want 3", len(inputs))
	}
	for index, eventType := range []string{"deployment_run.started", "deployment_run.failed", "deployment.paused"} {
		if inputs[index].EventType != eventType {
			t.Fatalf("scheduledFailureWebhookInputs()[%d].EventType = %q, want %q", index, inputs[index].EventType, eventType)
		}
	}
	if inputs[2].ResourceID != "depl_test" {
		t.Fatalf("deployment.paused resource ID = %q, want deployment ID", inputs[2].ResourceID)
	}
	if withoutPause := scheduledFailureWebhookInputs("drun_test", "depl_test", false); len(withoutPause) != 2 {
		t.Fatalf("scheduledFailureWebhookInputs(without pause) length = %d, want 2", len(withoutPause))
	}
}

package deployments

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPrepareDeploymentExecutionCarriesOutcomeDefinition(t *testing.T) {
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	deployment := db.Deployment{
		ExternalID:       "dpmt_test",
		OrganizationUUID: "org-test",
		WorkspaceUUID:    "ws-test",
		InitialEvents: json.RawMessage(`[{"type":"user.define_outcome","description":"ship it",` +
			`"rubric":{"type":"text","content":"# Rubric"},"max_iterations":4}]`),
	}
	prepared, err := prepareDeploymentExecution(deployment, "key-test", now)
	if err != nil {
		t.Fatalf("prepareDeploymentExecution: %v", err)
	}
	var outcomes []map[string]any
	if err := json.Unmarshal(prepared.Session.Session.OutcomeEvaluations, &outcomes); err != nil {
		t.Fatalf("decode outcome_evaluations: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome_evaluations = %d, want 1", len(outcomes))
	}
	first := outcomes[0]
	if first["description"] != "ship it" {
		t.Fatalf("description = %v, want ship it", first["description"])
	}
	rubric := first["rubric"].(map[string]any)
	if rubric["type"] != "text" || rubric["content"] != "# Rubric" {
		t.Fatalf("rubric = %v, want text/# Rubric", first["rubric"])
	}
	if first["max_iterations"] != float64(4) || first["status"] != "pending" {
		t.Fatalf("outcome = %v, want max_iterations=4 status=pending", first)
	}
}

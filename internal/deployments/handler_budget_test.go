package deployments

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

func TestDeploymentBudgetPatchRejectsAgentOnlyUnpricedModel(t *testing.T) {
	handler := &Handler{billing: billing.NewCalculator(map[string]billing.ModelPrice{
		"claude-sonnet-4-5-20250929": {InputPerMTok: 3, OutputPerMTok: 15},
	})}
	current := json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"500","currency":"USD"}}`)

	t.Run("agent-only patch with unpriced model is rejected", func(t *testing.T) {
		_, err := handler.deploymentBudgetPatch(current, nil, json.RawMessage(`{"model":"unpriced-model"}`))
		if err == nil || !strings.Contains(err.Error(), "unpriced-model") {
			t.Fatalf("deploymentBudgetPatch() error = %v, want unpriced model rejection", err)
		}
	})
	t.Run("agent-only patch with priced model keeps budget", func(t *testing.T) {
		budget, err := handler.deploymentBudgetPatch(current, nil, json.RawMessage(`{"model":"claude-sonnet-4-5-20250929"}`))
		if err != nil || string(budget) != string(current) {
			t.Fatalf("deploymentBudgetPatch() = %s, %v; want unchanged budget", budget, err)
		}
	})
	t.Run("no budget keeps snapshot without validation", func(t *testing.T) {
		budget, err := handler.deploymentBudgetPatch(nil, nil, json.RawMessage(`{"model":"unpriced-model"}`))
		if err != nil || budget != nil {
			t.Fatalf("deploymentBudgetPatch() = %s, %v; want nil budget without error", budget, err)
		}
	})
}

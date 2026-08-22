package models

import "testing"

func TestModelResponsesContainOnlyConfiguredIDs(t *testing.T) {
	models := modelResponses([]string{"kimi-k2.5", "qwen3-coder-plus"})
	if len(models) != 2 {
		t.Fatalf("modelResponses() len = %d", len(models))
	}
	if models[0].Type != "model" || models[0].ID != "kimi-k2.5" ||
		models[0].DisplayName != "kimi-k2.5" || models[0].CreatedAt != unknownModelCreatedAt ||
		models[0].Capabilities != nil || models[0].MaxInputTokens != nil || models[0].MaxTokens != nil ||
		models[1].Type != "model" || models[1].ID != "qwen3-coder-plus" ||
		models[1].DisplayName != "qwen3-coder-plus" || models[1].CreatedAt != unknownModelCreatedAt {
		t.Fatalf("models = %#v", models)
	}
}

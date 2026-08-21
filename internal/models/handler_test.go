package models

import "testing"

func TestModelResponsesContainOnlyConfiguredIDs(t *testing.T) {
	models := modelResponses([]string{"kimi-k2.5", "qwen3-coder-plus"})
	if len(models) != 2 {
		t.Fatalf("modelResponses() len = %d", len(models))
	}
	if models[0] != (modelResponse{Type: "model", ID: "kimi-k2.5"}) ||
		models[1] != (modelResponse{Type: "model", ID: "qwen3-coder-plus"}) {
		t.Fatalf("models = %#v", models)
	}
}

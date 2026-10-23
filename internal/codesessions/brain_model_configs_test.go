package codesessions

import "testing"

func TestTemporaryBrainModelConfigsEmptyCatalog(t *testing.T) {
	if configs := temporaryBrainModelConfigs(nil); configs == nil || len(configs) != 0 {
		t.Fatalf("configs=%v", configs)
	}
}

func TestTemporaryBrainModelConfigsFollowAvailableRoutes(t *testing.T) {
	backends := map[string]string{"provider/model": "messages", "model": "messages", "provider/child": "chat_completions"}
	configs := temporaryBrainModelConfigs(backends)
	if len(configs) != len(backends) {
		t.Fatalf("configs=%v", configs)
	}
	for model := range backends {
		if configs[model].ContextWindow != 1_000_000 || configs[model].MaxTokens != 256_000 {
			t.Fatalf("model=%s config=%+v", model, configs[model])
		}
	}
	delete(configs, "model")
	if len(temporaryBrainModelConfigs(backends)) != 3 {
		t.Fatal("temporary config maps must be independent")
	}
}

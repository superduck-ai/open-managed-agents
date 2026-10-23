package codesessions

type brainModelConfig struct {
	ContextWindow int `json:"context_window"`
	MaxTokens     int `json:"max_tokens"`
}

func temporaryBrainModelConfigs(backends map[string]string) map[string]brainModelConfig {
	configs := make(map[string]brainModelConfig, len(backends))
	for model := range backends {
		configs[model] = brainModelConfig{ContextWindow: 1_000_000, MaxTokens: 256_000}
	}
	return configs
}

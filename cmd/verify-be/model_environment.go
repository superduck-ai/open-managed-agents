package main

import (
	"slices"
	"strings"
)

func isolatedModelEnvironment(values []string) []string {
	return slices.DeleteFunc(slices.Clone(values), func(value string) bool {
		name, _, _ := strings.Cut(value, "=")
		if name == "GOOGLE_API_KEY" || name == "AWS_BEARER_TOKEN_BEDROCK" {
			return true
		}
		for _, prefix := range []string{"ANTHROPIC_", "CLAUDE_", "OPENAI_", "AZURE_OPENAI_", "GEMINI_", "GOOGLE_GENAI_", "VERTEX_", "BEDROCK_", "COHERE_", "MISTRAL_", "GROQ_", "DEEPSEEK_", "TOGETHER_", "OPENROUTER_", "OLLAMA_", "LLM_"} {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	})
}

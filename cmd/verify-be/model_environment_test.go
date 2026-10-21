package main

import (
	"slices"
	"strings"
	"testing"
)

func TestVerificationDoesNotInheritModelEnvironment(t *testing.T) {
	keys := []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_PROFILE", "ANTHROPIC_CUSTOM_HEADERS", "CLAUDE_CODE_USE_BEDROCK", "OPENAI_API_KEY", "OPENAI_BASE_URL", "AZURE_OPENAI_ENDPOINT", "GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "VERTEX_PROJECT", "BEDROCK_MODEL", "AWS_BEARER_TOKEN_BEDROCK", "COHERE_API_KEY", "MISTRAL_API_KEY", "GROQ_API_KEY", "DEEPSEEK_API_KEY", "TOGETHER_API_KEY", "OPENROUTER_API_KEY", "OLLAMA_HOST", "LLM_MODEL"}
	for _, key := range keys {
		t.Setenv(key, "external-model-setting")
	}
	e := newEnvironment("root", t.TempDir(), "test-run")
	for _, values := range [][]string{e.env(), isolatedSuiteEnvironment(e.env())} {
		for _, value := range values {
			key, _, _ := strings.Cut(value, "=")
			if slices.Contains(keys, key) {
				t.Fatalf("inherited model environment variable %s", key)
			}
		}
	}
	if !slices.Contains(e.env(), "TEST_API_KEY=sk-ant-local-default") {
		t.Fatal("isolated API credential missing")
	}
}

func TestModelIsolationPreservesInfrastructureEnvironment(t *testing.T) {
	values := []string{"PATH=/usr/bin", "HOME=/home/test", "DOCKER_HOST=unix:///docker.sock", "AWS_REGION=us-east-1", "VERIFY_BE_CLOUD_CONFIG=/private/config.json"}
	if !slices.Equal(isolatedModelEnvironment(values), values) {
		t.Fatal("infrastructure environment changed")
	}
}

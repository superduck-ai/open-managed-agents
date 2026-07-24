package modelmapping_test

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/modelmapping"
)

func TestValidateRejectsChainedMappings(t *testing.T) {
	err := modelmapping.Validate(map[string]string{
		"claude-sonnet-4-6": "glm-5-turbo",
		"glm-5-turbo":       "glm-5.2",
	})
	if err == nil {
		t.Fatal("Validate() error = nil, want chained mapping error")
	}
}

func TestResolvePreservesUnmappedModelAndUsesNonBlankMapping(t *testing.T) {
	mappings := map[string]string{
		"claude-sonnet-4-6": "glm-5-turbo",
		"claude-opus-4-8":   " ",
	}
	if got := modelmapping.Resolve("claude-sonnet-4-6", mappings); got != "glm-5-turbo" {
		t.Fatalf("Resolve(mapped) = %q, want glm-5-turbo", got)
	}
	if got := modelmapping.Resolve("claude-opus-4-8", mappings); got != "claude-opus-4-8" {
		t.Fatalf("Resolve(blank) = %q, want original ID", got)
	}
	if got := modelmapping.Resolve("claude-haiku-4-5", mappings); got != "claude-haiku-4-5" {
		t.Fatalf("Resolve(unmapped) = %q, want original ID", got)
	}
	if got := modelmapping.Resolve(" claude-sonnet-4-6 ", mappings); got != "glm-5-turbo" {
		t.Fatalf("Resolve(padded mapped) = %q, want glm-5-turbo", got)
	}
	if got := modelmapping.Resolve(" claude-haiku-4-5 ", mappings); got != "claude-haiku-4-5" {
		t.Fatalf("Resolve(padded unmapped) = %q, want trimmed original ID", got)
	}
}

func TestValidateAllowsAliasesToShareAnUpstreamModel(t *testing.T) {
	if err := modelmapping.Validate(map[string]string{
		"claude-sonnet-4-6":        "glm-5-turbo",
		"claude-sonnet-4-5-latest": "glm-5-turbo",
	}); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

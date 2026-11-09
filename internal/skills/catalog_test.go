package skills

import "testing"

func TestSkillMetadataRejectsMalformedYAMLFields(t *testing.T) {
	metadata := parseSkillMetadata([]byte("---\nname: [broken\n---\n# Fallback\n\nFallback description"), "directory")
	if metadata.Name != "Fallback" || metadata.Description != "Fallback description" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestSkillMetadataParsesMultilineYAMLWithNestedMetadata(t *testing.T) {
	metadata := parseSkillMetadata([]byte("---\nname: pdf\ndescription: >-\n  Read PDF\n  documents\nlicense: MIT\nmetadata:\n  owner: team\ndisable-model-invocation: true\n---\n# Heading\n\nInstructions"), "directory")
	if metadata.Name != "pdf" || metadata.Description != "Read PDF documents" || metadata.License != "MIT" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

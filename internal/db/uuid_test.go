package db

import (
	"testing"

	"github.com/google/uuid"
)

func TestParseDBUUIDRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "not-a-uuid", uuid.Nil.String()} {
		if _, err := parseDBUUID("workspace_uuid", value); err == nil {
			t.Fatalf("parseDBUUID(%q) error = nil, want validation error", value)
		}
	}
}

func TestTryParseDBUUIDIdentifierStringPreservesCompatibilityLookup(t *testing.T) {
	valid := tryParseDBUUIDIdentifierString("11111111-1111-4111-8111-111111111111")
	if valid == "" {
		t.Fatal("tryParseDBUUIDIdentifierString(valid UUID) returned empty")
	}
	for _, value := range []string{"", "external_id", uuid.Nil.String()} {
		if got := tryParseDBUUIDIdentifierString(value); got != "" {
			t.Fatalf("tryParseDBUUIDIdentifierString(%q) = %q, want empty", value, got)
		}
	}
}

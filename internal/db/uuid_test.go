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

func TestTypedDBUUIDArgumentsRejectsInvalidValues(t *testing.T) {
	_, err := typedDBUUIDArguments(map[string]any{
		"workspace_uuid": dbUUID("not-a-uuid"),
	})
	if err == nil {
		t.Fatal("typedDBUUIDArguments(invalid UUID) error = nil")
	}
}

func TestTypedDBUUIDArgumentsUsesStandardUUIDTypes(t *testing.T) {
	required := "11111111-1111-4111-8111-111111111111"
	nullable := "22222222-2222-4222-8222-222222222222"
	value, err := typedDBUUIDArguments(map[string]any{
		"workspace_uuid": dbUUID(required),
		"user_uuid":      dbNullableUUID(&nullable),
		"api_key_uuid":   dbNullableUUID(nil),
		"name":           "unchanged",
	})
	if err != nil {
		t.Fatalf("typedDBUUIDArguments() error = %v", err)
	}
	if got, ok := value["workspace_uuid"].(uuid.UUID); !ok || got.String() != required {
		t.Fatalf("workspace_uuid = %#v, want uuid.UUID(%s)", value["workspace_uuid"], required)
	}
	if got, ok := value["user_uuid"].(uuid.NullUUID); !ok || !got.Valid || got.UUID.String() != nullable {
		t.Fatalf("user_uuid = %#v, want valid uuid.NullUUID(%s)", value["user_uuid"], nullable)
	}
	if got, ok := value["api_key_uuid"].(uuid.NullUUID); !ok || got.Valid {
		t.Fatalf("api_key_uuid = %#v, want invalid uuid.NullUUID", value["api_key_uuid"])
	}
	if got := value["name"]; got != "unchanged" {
		t.Fatalf("name = %#v, want unchanged", got)
	}
}

func TestTryParseDBUUIDIdentifierPreservesCompatibilityLookup(t *testing.T) {
	valid := tryParseDBUUIDIdentifier("11111111-1111-4111-8111-111111111111")
	if !valid.Valid {
		t.Fatal("tryParseDBUUIDIdentifier(valid UUID) returned null")
	}
	for _, value := range []string{"", "external_id", uuid.Nil.String()} {
		if got := tryParseDBUUIDIdentifier(value); got.Valid {
			t.Fatalf("tryParseDBUUIDIdentifier(%q) = %+v, want null", value, got)
		}
	}
}

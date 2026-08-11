package vaults

import (
	"encoding/json"
	"testing"
)

func TestPatchDisplayNameRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "null", raw: json.RawMessage(`null`)},
		{name: "non string", raw: json.RawMessage(`42`)},
		{name: "empty", raw: json.RawMessage(`""`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := patchDisplayName("current", tt.raw); err == nil {
				t.Fatal("patchDisplayName() error = nil")
			}
		})
	}
}

func TestPatchDisplayNamePreservesMissingAndUpdatesPresent(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "missing", want: "current"},
		{name: "present", raw: json.RawMessage(`"next"`), want: "next"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := patchDisplayName("current", tt.raw)
			if err != nil {
				t.Fatalf("patchDisplayName() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("patchDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

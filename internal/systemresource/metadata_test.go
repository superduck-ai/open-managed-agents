package systemresource

import (
	"encoding/json"
	"testing"
)

func TestDreamResourceMetadata(t *testing.T) {
	tests := []struct {
		name            string
		metadata        json.RawMessage
		wantKind        string
		wantAgent       bool
		wantEnvironment bool
	}{
		{name: "Dream agent", metadata: json.RawMessage(`{"internal_kind":"dream_default_agent"}`), wantKind: DreamDefaultAgentKind, wantAgent: true},
		{name: "Dream environment", metadata: json.RawMessage(`{"internal_kind":"dream_default_environment"}`), wantKind: DreamDefaultEnvironmentKind, wantEnvironment: true},
		{name: "ordinary resource", metadata: json.RawMessage(`{"team":"memory"}`)},
		{name: "invalid metadata", metadata: json.RawMessage(`{`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Kind(test.metadata); got != test.wantKind {
				t.Fatalf("Kind() = %q, want %q", got, test.wantKind)
			}
			if got := IsDreamDefaultAgent(test.metadata); got != test.wantAgent {
				t.Fatalf("IsDreamDefaultAgent() = %t, want %t", got, test.wantAgent)
			}
			if got := IsDreamDefaultEnvironment(test.metadata); got != test.wantEnvironment {
				t.Fatalf("IsDreamDefaultEnvironment() = %t, want %t", got, test.wantEnvironment)
			}
		})
	}
}

func TestReservedKinds(t *testing.T) {
	for _, kind := range []string{DreamDefaultAgentKind, DreamDefaultEnvironmentKind} {
		if !IsReservedKind(kind) {
			t.Fatalf("IsReservedKind(%q) = false, want true", kind)
		}
	}
	if IsReservedKind("customer_resource") {
		t.Fatal("IsReservedKind(customer_resource) = true, want false")
	}
}

package environments

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func TestMemoryCatalogRejectsInvalidSnapshots(t *testing.T) {
	for _, store := range []memoryStoreDefinition{
		{Name: "test", MountPath: "/etc/store", Access: "read_only"},
		{Name: "test", MountPath: "/mnt/memory/../store", Access: "read_only"},
		{Name: "test", MountPath: "/mnt/memory/a/b", Access: "read_only"},
		{Name: "test", MountPath: "/mnt/memory/a\\b", Access: "read_only"},
		{Name: "test", MountPath: "/mnt/memory/a", Access: "rw"},
		{Name: "test", MountPath: "/mnt/memory/a", Access: "read_only", Instructions: strings.Repeat("中", 4097)},
	} {
		if _, err := memoryCatalogPrompt([]memoryStoreDefinition{store}); !errors.Is(err, errMemorySnapshotInvalid) {
			t.Fatalf("accepted invalid store: %+v", store)
		}
	}
	store := memoryStoreDefinition{Name: "test", MountPath: "/mnt/memory/a", Access: "read_only"}
	if _, err := memoryCatalogPrompt([]memoryStoreDefinition{store, store}); !errors.Is(err, errMemorySnapshotInvalid) {
		t.Fatal("duplicate store accepted")
	}
	stores := make([]memoryStoreDefinition, sessionresource.MaxMemoryStores+1)
	if _, err := memoryCatalogPrompt(stores); !errors.Is(err, errMemorySnapshotInvalid) {
		t.Fatal("oversized store catalog accepted")
	}
}

func TestMemoryCatalogUsesAuthoritativeConfigurationAndRecovery(t *testing.T) {
	mounts := []memoryRuntimeMount{{Name: "notes", MountPath: "/mnt/memory/notes", Access: sessionresource.MemoryAccessReadOnly, Description: "team notes", Instructions: "read when needed"}}
	session := db.Session{AgentSnapshot: json.RawMessage(`{"system":"agent","sandbox_memory_stores":[{"name":"forged"}]}`)}
	configuration, err := managedAgentSessionConfig(session, managedAgentRuntimeResources{memoryMounts: mounts}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configuration), "forged") || !strings.Contains(string(configuration), "read when needed") || !strings.Contains(string(configuration), "attached_memory_stores") {
		t.Fatalf("invalid catalog: %s", configuration)
	}
	mounts[0].Instructions = "changed"
	if strings.Contains(string(configuration), "changed") {
		t.Fatal("pinned configuration mutated")
	}
}

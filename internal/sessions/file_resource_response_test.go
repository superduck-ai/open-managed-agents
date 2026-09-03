package sessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestResponseFromResourceHandlesMissingExplicitConfig(t *testing.T) {
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	resource := db.SessionResource{
		ExternalID:   "sesrsc_null_1",
		ResourceType: "memory_store",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	raw := responseFromResource(resource)

	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析响应：%v", err)
	}
	assertFixtureResourceString(t, out, "id", "sesrsc_null_1")
	assertFixtureResourceString(t, out, "type", "memory_store")
}

func TestResponseFromResourceBackfillsOutputFileID(t *testing.T) {
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	resource := db.SessionResource{
		ExternalID:   "sesrsc_output_1",
		ResourceType: "file",
		File: &db.SessionResourceFileReference{
			FileID:        "file_owned_1",
			NamespacePath: "/outputs/report.pdf",
			MountPath:     "/outputs/report.pdf",
			Ownership:     db.SessionResourceFileOwnershipOwned,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	raw := responseFromResource(resource)

	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析响应：%v", err)
	}
	assertFixtureResourceString(t, out, "id", "sesrsc_output_1")
	assertFixtureResourceString(t, out, "type", "file")
	assertFixtureResourceString(t, out, "file_id", "file_owned_1")
	assertFixtureResourceString(t, out, "mount_path", "/mnt/user-data/outputs/report.pdf")
}

func TestResponseFromResourceKeepsInputResourceFields(t *testing.T) {
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	resource := db.SessionResource{
		ExternalID:   "sesrsc_input_1",
		ResourceType: "file",
		File: &db.SessionResourceFileReference{
			FileID:        "file_uploaded_1",
			NamespacePath: "/uploads/data.csv",
			MountPath:     "/uploads/data.csv",
			Ownership:     db.SessionResourceFileOwnershipReferenced,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	raw := responseFromResource(resource)

	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析响应：%v", err)
	}
	assertFixtureResourceString(t, out, "file_id", "file_uploaded_1")
	assertFixtureResourceString(t, out, "mount_path", "/mnt/session/uploads/data.csv")
	if _, exists := out["source"]; exists {
		t.Fatalf("Input Resource 泄漏内部 source 字段：%s", raw)
	}
}

func TestResponseFromResourceLeavesNonFileResourcesUntouched(t *testing.T) {
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	resource := db.SessionResource{
		ExternalID:   "sesrsc_dir_1",
		ResourceType: "directory",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	raw := responseFromResource(resource)

	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析响应：%v", err)
	}
	assertFixtureResourceString(t, out, "id", "sesrsc_dir_1")
	assertFixtureResourceString(t, out, "type", "directory")
	if _, exists := out["file_id"]; exists {
		t.Fatalf("directory 资源不应有 file_id：%s", raw)
	}
}

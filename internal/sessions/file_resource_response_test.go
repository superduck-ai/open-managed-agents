package sessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestResponseFromResourceHandlesNullPayload(t *testing.T) {
	// payload 为 JSON null 时 Unmarshal 成功但 map 为 nil，不做守卫会 panic。
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	resource := db.SessionResource{
		ExternalID:   "sesrsc_null_1",
		ResourceType: "memory_store",
		Payload:      json.RawMessage(`null`),
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
		ExternalID:     "sesrsc_output_1",
		ResourceType:   "file",
		Payload:        nil,
		Path:           "/outputs/report.pdf",
		FileExternalID: "file_owned_1",
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	raw := responseFromResource(resource)

	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析响应：%v", err)
	}
	assertFixtureResourceString(t, out, "id", "sesrsc_output_1")
	assertFixtureResourceString(t, out, "type", "file")
	assertFixtureResourceString(t, out, "file_id", "file_owned_1")
	assertFixtureResourceString(t, out, "mount_path", "/outputs/report.pdf")
}

func TestResponseFromResourceKeepsInputResourcePayload(t *testing.T) {
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	resource := db.SessionResource{
		ExternalID:   "sesrsc_input_1",
		ResourceType: "file",
		Payload: json.RawMessage(`{
			"id": "sesrsc_input_1",
			"type": "file",
			"file_id": "file_uploaded_1",
			"source": "/uploads",
			"mount_path": "/uploads/data.csv"
		}`),
		CreatedAt: now,
		UpdatedAt: now,
	}

	raw := responseFromResource(resource)

	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析响应：%v", err)
	}
	assertFixtureResourceString(t, out, "file_id", "file_uploaded_1")
	assertFixtureResourceString(t, out, "mount_path", "/uploads/data.csv")
	if _, exists := out["source"]; exists {
		t.Fatalf("Input Resource 泄漏内部 source 字段：%s", raw)
	}
}

func TestResponseFromResourceLeavesNonFileResourcesUntouched(t *testing.T) {
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	resource := db.SessionResource{
		ExternalID:   "sesrsc_dir_1",
		ResourceType: "directory",
		Payload:      json.RawMessage(`{"id":"sesrsc_dir_1","type":"directory","path":"/outputs"}`),
		// 目录即使位于 /outputs/ 下也不应被当成输出文件回填。
		Path:           "/outputs/reports",
		FileExternalID: "file_owned_1",
		CreatedAt:      now,
		UpdatedAt:      now,
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

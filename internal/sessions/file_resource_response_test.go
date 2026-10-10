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
	assertResourceString(t, out, "id", "sesrsc_null_1")
	assertResourceString(t, out, "type", "memory_store")
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
	assertResourceString(t, out, "id", "sesrsc_output_1")
	assertResourceString(t, out, "type", "file")
	assertResourceString(t, out, "file_id", "file_owned_1")
	assertResourceString(t, out, "mount_path", "/outputs/report.pdf")
	if _, exists := out["source"]; exists {
		t.Fatalf("file resource leaked source: %s", raw)
	}
	for _, field := range []string{"created_at", "updated_at"} {
		if _, exists := out[field]; !exists {
			t.Fatalf("file resource missing %s: %s", field, raw)
		}
	}
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
	assertResourceString(t, out, "file_id", "file_uploaded_1")
	assertResourceString(t, out, "mount_path", "/uploads/data.csv")
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
	assertResourceString(t, out, "id", "sesrsc_dir_1")
	assertResourceString(t, out, "type", "directory")
	if _, exists := out["file_id"]; exists {
		t.Fatalf("directory 资源不应有 file_id：%s", raw)
	}
}

func assertResourceString(t *testing.T, resource map[string]json.RawMessage, field, expected string) {
	t.Helper()
	var actual string
	if err := json.Unmarshal(resource[field], &actual); err != nil {
		t.Fatalf("解析 fixture File Resource 的 %s 字段：%v", field, err)
	}
	if actual != expected {
		t.Fatalf("fixture File Resource 的 %s = %q，期望 %q", field, actual, expected)
	}
}

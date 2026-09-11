package sessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestFixtureFileResourceMatchesOfficialResponseContract(t *testing.T) {
	handler := Handler{cfg: config.Config{SDKFixtures: config.SDKFixtureConfig{
		SessionResourceID: "sesrsc_fixture",
	}}}

	var resource map[string]json.RawMessage
	if err := json.Unmarshal(handler.fixtureResource(time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC)), &resource); err != nil {
		t.Fatalf("解析 fixture File Resource：%v", err)
	}

	if _, exists := resource["source"]; exists {
		t.Fatalf("fixture File Resource 泄漏内部 source 字段：%s", handler.fixtureResource(time.Time{}))
	}
	assertFixtureResourceString(t, resource, "id", "sesrsc_fixture")
	assertFixtureResourceString(t, resource, "file_id", "file_011CNha8iCJcU1wXNR6q4V8w")
	assertFixtureResourceString(t, resource, "mount_path", "/mnt/session/uploads/receipt.pdf")
	assertFixtureResourceString(t, resource, "type", "file")

	for _, field := range []string{"created_at", "updated_at"} {
		if _, exists := resource[field]; !exists {
			t.Fatalf("fixture File Resource 缺少 %s 字段：%v", field, resource)
		}
	}
}

func assertFixtureResourceString(t *testing.T, resource map[string]json.RawMessage, field, expected string) {
	t.Helper()
	var actual string
	if err := json.Unmarshal(resource[field], &actual); err != nil {
		t.Fatalf("解析 fixture File Resource 的 %s 字段：%v", field, err)
	}
	if actual != expected {
		t.Fatalf("fixture File Resource 的 %s = %q，期望 %q", field, actual, expected)
	}
}

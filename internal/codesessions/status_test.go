package codesessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionUsagePayload(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	record := db.CodeSession{ExternalID: "csev_test"}

	session := db.Session{
		Usage: json.RawMessage(`{"input_tokens":100,"output_tokens":50}`),
	}
	payload, err := sessionUsagePayload(record, session, now)
	if err != nil {
		t.Fatalf("sessionUsagePayload: %v", err)
	}
	object := decodePayload(t, payload)
	if object["type"] != "session.usage" {
		t.Fatalf("type = %v, want session.usage", object["type"])
	}
	usage, ok := object["usage"].(map[string]any)
	if !ok || usage["input_tokens"] != float64(100) {
		t.Fatalf("usage = %v, want 含 input_tokens", object["usage"])
	}
	if object["budget"] != nil {
		t.Fatalf("budget = %v, want null", object["budget"])
	}

	empty := db.Session{}
	payload, err = sessionUsagePayload(record, empty, now)
	if err != nil {
		t.Fatalf("sessionUsagePayload: %v", err)
	}
	object = decodePayload(t, payload)
	if usage, ok := object["usage"].(map[string]any); !ok || len(usage) != 0 {
		t.Fatalf("无 usage 应为空对象, got %v", object["usage"])
	}
}

func TestStatusTransitionPayloadsUsagePrecedesIdle(t *testing.T) {
	// 官方契约：usage 紧邻 idle 之前，且落库后（timestamptz 微秒精度）顺序仍成立。
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	record := db.CodeSession{ExternalID: "csev_test"}
	session := db.Session{Usage: json.RawMessage(`{"input_tokens":1}`), UpdatedAt: now}

	payloads, err := statusTransitionPayloads(record, session, "session.status_idle", "idle", now)
	if err != nil {
		t.Fatalf("statusTransitionPayloads: %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("payloads 数量 = %d, want 2", len(payloads))
	}
	first := decodePayload(t, payloads[0])
	second := decodePayload(t, payloads[1])
	if first["type"] != "session.usage" || second["type"] != "session.status_idle" {
		t.Fatalf("顺序错误: %v then %v, want usage then idle", first["type"], second["type"])
	}
	usageAt := parsePayloadTime(t, first, "created_at")
	idleAt := parsePayloadTime(t, second, "created_at")
	if !usageAt.Before(idleAt) {
		t.Fatalf("usage created_at = %s, 应严格早于 idle created_at = %s", usageAt, idleAt)
	}
	if idleAt.Sub(usageAt) < time.Microsecond {
		t.Fatalf("时间差 = %s, 至少需要 1µs 以在 timestamptz 落库后保序", idleAt.Sub(usageAt))
	}
	if parsePayloadTime(t, first, "processed_at") != usageAt {
		t.Fatalf("usage processed_at 应与 created_at 一致")
	}
}

func TestStatusTransitionPayloadsNonIdleOmitsUsage(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	record := db.CodeSession{ExternalID: "csev_test"}

	payloads, err := statusTransitionPayloads(record, db.Session{UpdatedAt: now}, "session.status_running", "running", now)
	if err != nil {
		t.Fatalf("statusTransitionPayloads: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads 数量 = %d, want 1", len(payloads))
	}
	if payload := decodePayload(t, payloads[0]); payload["type"] != "session.status_running" {
		t.Fatalf("type = %v, want session.status_running", payload["type"])
	}
}

func decodePayload(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return object
}

func parsePayloadTime(t *testing.T, payload map[string]any, field string) time.Time {
	t.Helper()
	value, ok := payload[field].(string)
	if !ok {
		t.Fatalf("%s = %v, want 字符串", field, payload[field])
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse %s: %v", field, err)
	}
	return parsed.UTC()
}

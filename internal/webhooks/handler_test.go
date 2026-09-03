package webhooks

import (
	"encoding/json"
	"testing"
)

func TestParseEnabledEventsRejectsUnknownEvent(t *testing.T) {
	// 失败场景先行：未知事件类型必须 400
	raw := json.RawMessage(`["session.not_a_real_event"]`)
	_, err := parseEnabledEvents(raw)
	if err == nil {
		t.Fatal("parseEnabledEvents 未知事件应报错")
	}
}

func TestParseEnabledEventsRejectsDuplicateEvent(t *testing.T) {
	raw := json.RawMessage(`["session.updated","session.updated"]`)
	_, err := parseEnabledEvents(raw)
	if err == nil {
		t.Fatal("parseEnabledEvents 重复事件应报错")
	}
}

func TestParseEnabledEventsErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
	}{
		{"empty", json.RawMessage(``)},
		{"not array", json.RawMessage(`"session.updated"`)},
		{"empty array", json.RawMessage(`[]`)},
		{"empty string", json.RawMessage(`[""]`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseEnabledEvents(tc.raw)
			if err == nil {
				t.Fatalf("parseEnabledEvents(%s) 应报错", tc.raw)
			}
		})
	}
}

func TestParseEnabledEventsAcceptsSessionLifecycleEvents(t *testing.T) {
	// 失败场景先行：修复前 session.created/pending/archived 不在允许列表
	// （回归守卫：这三个 OMA 扩展事件必须可订阅）
	for _, eventType := range []string{
		"session.created",
		"session.pending",
		"session.archived",
		"session.status_run_started",
		"session.status_idled",
		"session.deleted",
		"vault_credential.refresh_failed",
	} {
		t.Run(eventType, func(t *testing.T) {
			raw, err := json.Marshal([]string{eventType})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got, err := parseEnabledEvents(raw)
			if err != nil {
				t.Fatalf("parseEnabledEvents(%s) 应成功: %v", eventType, err)
			}
			if len(got) != 1 || got[0] != eventType {
				t.Fatalf("parseEnabledEvents(%s) = %v, want [%s]", eventType, got, eventType)
			}
		})
	}
}

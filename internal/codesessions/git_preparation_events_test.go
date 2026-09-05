package codesessions

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGitPreparationEventsUsePublicSystemMessages(t *testing.T) {
	for _, state := range []string{"started", "ready", "failed", "unknown"} {
		t.Run(state, func(t *testing.T) {
			raw := json.RawMessage(`{"type":"env_manager_log","uuid":"git-event","data":{"content":"sensitive raw stderr","extra":{"resource_type":"git_repository","status":"` + state + `","url":"https://github.com/octocat/Hello-World","mount_path":"/workspace/repo","token":"must-not-leak"}}}`)
			normalized, err := normalizeWorkerOutboundPayload("cse_git", raw, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Type    string `json:"type"`
				Subtype string `json:"subtype"`
			}
			if err := json.Unmarshal(normalized, &payload); err != nil {
				t.Fatal(err)
			}
			if state == "unknown" {
				if payload.Type != "env_manager_log" {
					t.Fatal("unknown log became public")
				}
				return
			}
			if payload.Type != "system.message" || payload.Subtype != "git_repository" || !isPublicWorkerOutputEvent(payload.Type) {
				t.Fatalf("unexpected public event: %s", normalized)
			}
			if strings.Contains(string(normalized), "sensitive") || strings.Contains(string(normalized), "must-not-leak") {
				t.Fatal("raw log data entered public history")
			}
		})
	}
}

func TestGitPreparationDetailsWhitelist(t *testing.T) {
	for _, reason := range []string{"access_denied", "network_error", "checkout_failed", "path_conflict", "timed_out", "cancelled", "unknown", "secret-token"} {
		fields := gitPreparationPublicFields(map[string]json.RawMessage{"data": json.RawMessage(`{"extra":{"resource_type":"git_repository","status":"failed","url":"https://github.com/owner/repo","mount_path":"/workspace/repo","duration_ms":1234,"failure_reason":"` + reason + `"}}`)})
		if string(fields["duration_ms"]) != "1234" {
			t.Fatal("duration missing")
		}
		if reason == "secret-token" {
			if _, ok := fields["failure_reason"]; ok {
				t.Fatal("unrecognized reason leaked")
			}
		} else if string(fields["failure_reason"]) != `"`+reason+`"` {
			t.Fatal("reason missing")
		}
	}
}

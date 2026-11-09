package managedagentsevents

import "testing"

func TestEqualPublicationPayload(t *testing.T) {
	for _, test := range []struct {
		name, kind, left, right string
		equal                   bool
	}{
		{"content conflict", "agent.message", `{"content":[{"text":"a"}]}`, `{"content":[{"text":"b"}]}`, false},
		{"missing content", "agent.message", `{}`, `{"content":[]}`, false},
		{"tool conflict", "agent.tool_result", `{"is_error":true}`, `{"is_error":false}`, false},
		{"proxy metadata", "agent.message", `{"content":[{"type":"text","text":"a"}],"model_request_start_id":"request","processed_at":"old","_echo_key":"key"}`, `{"content":[{"text":"a","type":"text"}]}`, true},
		{"thinking content is private", "agent.thinking", `{"content":[{"text":"private"}]}`, `{}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			equal, err := EqualPublicationPayload(test.kind, []byte(test.left), []byte(test.right))
			if err != nil || equal != test.equal {
				t.Fatalf("equal=%v err=%v", equal, err)
			}
		})
	}
}

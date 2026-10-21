package codesessions

import (
	"strings"
	"testing"
)

func TestSandboxMCPReplyOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, payload, outcome string
		reply                  bool
	}{
		{"invalid json", `{`, "", false},
		{"notification", `{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`, "", false},
		{"no result", `{"jsonrpc":"2.0","id":1}`, "", false},
		{"rpc error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad"}}`, "rpc_error", true},
		{"tool error", `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[]}}`, "tool_error", true},
		{"success", `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`, "ok", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcome, reply := sandboxMCPReplyOutcome([]byte(tc.payload))
			if outcome != tc.outcome || reply != tc.reply {
				t.Fatalf("outcome = %q, %v", outcome, reply)
			}
		})
	}
}

func TestSandboxMCPStreamInspector(t *testing.T) {
	for _, tc := range []struct {
		name    string
		chunks  []string
		outcome string
	}{
		{"oversized event ignored", []string{"data: " + strings.Repeat("x", sandboxMCPInspectLimit+1) + "\n\n"}, ""},
		{"progress only", []string{"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n"}, ""},
		{"tool error split across chunks", []string{"data: {\"jsonrpc\":\"2.0\",\"id\":1,", "\"result\":{\"isError\":true}}\r\n", "\r\n"}, "tool_error"},
		{"last reply wins", []string{"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"}, "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inspector := &sandboxMCPStreamInspector{}
			for _, chunk := range tc.chunks {
				inspector.observe([]byte(chunk))
			}
			inspector.observe([]byte("\n\n"))
			if inspector.outcome != tc.outcome {
				t.Fatalf("outcome = %q", inspector.outcome)
			}
		})
	}
}

func TestSandboxMCPToolName(t *testing.T) {
	for _, tc := range []struct{ method, name, want string }{
		{"tools/list", "bash", ""},
		{"tools/call", "private-tool", "other"},
		{"tools/call", "bash", "bash"},
	} {
		call := sandboxRPCRequest{Method: tc.method}
		call.Params.Name = tc.name
		if got := sandboxMCPToolName(call); got != tc.want {
			t.Fatalf("%s %s = %q", tc.method, tc.name, got)
		}
	}
}

package codesessions

import "testing"

func TestOAuthRequestValidation(t *testing.T) {
	for _, input := range []OAuthRequest{
		{},
		{Kind: "env_oauth", Provider: "custom:friday", SecretName: "FRIDAY_4A_TOKEN", Reason: "missing"},
		{Kind: "env_oauth", ParentCallID: "call", Provider: "custom:friday", Reason: "missing"},
		{Kind: "mcp_oauth", ParentCallID: "call", MCPServerName: "notion", MCPServerURL: "https://user:secret@example.com/mcp", Reason: "missing"},
		{Kind: "mcp_oauth", ParentCallID: "call", MCPServerName: "notion", MCPServerURL: "https://example.com/mcp", Reason: "forbidden"},
	} {
		if err := input.Validate(); err == nil {
			t.Fatalf("应拒绝无效授权需求：%+v", input)
		}
	}
	for _, input := range []OAuthRequest{
		{Kind: "env_oauth", ParentCallID: "bash-call", Provider: "custom:friday", SecretName: "FRIDAY_4A_TOKEN", Reason: "missing"},
		{Kind: "mcp_oauth", ParentCallID: "mcp-call", MCPServerName: "notion", MCPServerURL: "https://mcp.notion.com/mcp", Reason: "reauthorization_required"},
	} {
		if err := input.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

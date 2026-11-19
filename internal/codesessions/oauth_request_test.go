package codesessions

import (
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

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
		{Kind: "env_oauth", ParentCallID: "bash-call", TargetHost: "friday.example", SecretName: "FRIDAY_4A_TOKEN", Reason: "missing"},
		{Kind: "env_oauth", ParentCallID: "bash-call", Provider: "custom:friday", SecretName: "FRIDAY_4A_TOKEN", Reason: "missing"},
		{Kind: "mcp_oauth", ParentCallID: "mcp-call", MCPServerName: "notion", MCPServerURL: "https://mcp.notion.com/mcp", Reason: "reauthorization_required"},
	} {
		if err := input.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInlineOAuthProviderNameUsesCharacterLimit(t *testing.T) {
	request := OAuthRequest{
		Kind: "env_oauth", Provider: vaults.EnvOAuthProviderInline, SecretName: "FRIDAY_4A_TOKEN",
		ParentCallID: "bash-call", Reason: "reauthorization_required",
		ProviderName: strings.Repeat("授", vaults.EnvOAuthInlineNameMaxRunes+1),
	}
	if err := request.Validate(); err == nil {
		t.Fatal("超过字符限制的名称校验通过")
	}
	request.ProviderName = strings.Repeat("授", vaults.EnvOAuthInlineNameMaxRunes)
	if err := request.Validate(); err != nil {
		t.Fatalf("合法的中文 OAuth 名称不能重新授权：%v", err)
	}
}

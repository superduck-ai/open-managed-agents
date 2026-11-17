package feishu

import (
	"cmp"
	"encoding/json/v2"
	"net/url"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

func oauthElements(origin string, value contracts.ActionValue, inputJSON string) []map[string]any {
	var input struct {
		Provider      string `json:"provider"`
		ProviderName  string `json:"provider_name"`
		MCPServerName string `json:"mcp_server_name"`
	}
	if err := json.Unmarshal([]byte(inputJSON), &input); err != nil || origin == "" {
		return []map[string]any{{"tag": "markdown", "content": "需要连接目标服务，请在 OMA 中完成授权。"}}
	}
	if value.ChatType != "p2p" {
		return []map[string]any{{"tag": "markdown", "content": "个人服务授权请在私聊中完成。"}}
	}
	target := origin + "/oauth/channels/" + url.PathEscape(value.ChannelID) + "/sessions/" + url.PathEscape(value.SessionID) + "/requests/" + url.PathEscape(value.ToolUseID)
	value.Kind = "cancel_oauth"
	return []map[string]any{
		{"tag": "markdown", "content": "连接 " + cmp.Or(input.ProviderName, input.MCPServerName, input.Provider) + "，授权后继续当前操作。"},
		{"tag": "button", "type": "primary", "text": map[string]any{"tag": "plain_text", "content": "前往授权"}, "behaviors": []any{map[string]any{"type": "open_url", "default_url": target}}},
		{"tag": "button", "text": map[string]any{"tag": "plain_text", "content": "取消"}, "value": value},
	}
}

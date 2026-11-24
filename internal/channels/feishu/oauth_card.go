package feishu

import (
	"cmp"
	"encoding/json/v2"
	"net/url"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

func oauthElements(origin string, value contracts.ActionValue, inputJSON string, stop map[string]any) []map[string]any {
	var input struct {
		Provider      string `json:"provider"`
		ProviderName  string `json:"provider_name"`
		MCPServerName string `json:"mcp_server_name"`
	}
	var fallback string
	if err := json.Unmarshal([]byte(inputJSON), &input); err != nil || origin == "" {
		fallback = "需要连接目标服务，请在 OMA 中完成授权。"
	} else if value.ChatType != "p2p" {
		fallback = "个人服务授权请在私聊中完成。"
	}
	if fallback != "" {
		elements := []map[string]any{{"tag": "markdown", "content": fallback}}
		if stop != nil {
			elements = append(elements, stop)
		}
		return elements
	}
	target := origin + "/oauth/channels/" + url.PathEscape(value.ChannelID) + "/sessions/" + url.PathEscape(value.SessionID) + "/requests/" + url.PathEscape(value.ToolUseID)
	pairing := value.Kind == "pairing_oauth"
	if pairing {
		target = origin + "/oauth/channels/" + url.PathEscape(value.ChannelID) + "/pairing/" + url.PathEscape(value.SourceID)
	}
	value.Kind = "cancel_oauth"
	buttons := []map[string]any{
		{"tag": "button", "type": "primary", "text": map[string]any{"tag": "plain_text", "content": "前往授权"}, "behaviors": []any{map[string]any{"type": "open_url", "default_url": target}}},
		{"tag": "button", "text": map[string]any{"tag": "plain_text", "content": "取消"}, "value": value},
	}
	if pairing {
		buttons = buttons[:1]
	}
	if stop != nil {
		buttons = append(buttons, stop)
	}
	columns := make([]map[string]any, 0, len(buttons))
	for _, action := range buttons {
		columns = append(columns, map[string]any{"tag": "column", "width": "auto", "elements": []map[string]any{action}})
	}
	return []map[string]any{
		{"tag": "markdown", "content": "连接 " + cmp.Or(input.ProviderName, input.MCPServerName, input.Provider) + "，授权后继续当前操作。"},
		{"tag": "column_set", "flex_mode": "none", "horizontal_spacing": "8px", "columns": columns},
	}
}

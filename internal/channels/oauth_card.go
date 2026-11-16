package channels

import (
	"cmp"
	"encoding/json/v2"
	"net/url"
)

func (r *channelReply) oauthElements(value channelActionValue, event omaEvent) []map[string]any {
	var input struct {
		Provider      string `json:"provider"`
		ProviderName  string `json:"provider_name"`
		MCPServerName string `json:"mcp_server_name"`
	}
	if err := json.Unmarshal(event.Input, &input); err != nil || r.oauthOrigin == "" {
		return []map[string]any{{"tag": "markdown", "content": "需要连接目标服务，请在 OMA 中完成授权。"}}
	}
	if value.ChatType != "p2p" {
		return []map[string]any{{"tag": "markdown", "content": "个人服务授权请在私聊中完成。"}}
	}
	target := r.oauthOrigin + "/oauth/channels/" + url.PathEscape(value.ChannelID) + "/sessions/" + url.PathEscape(value.SessionID) + "/requests/" + url.PathEscape(event.ID)
	value.Kind = "cancel_oauth"
	return []map[string]any{
		{"tag": "markdown", "content": "连接 " + cmp.Or(input.ProviderName, input.MCPServerName, input.Provider) + "，授权后继续当前操作。"},
		{"tag": "button", "type": "primary", "text": map[string]any{"tag": "plain_text", "content": "前往授权"}, "behaviors": []any{map[string]any{"type": "open_url", "default_url": target}}},
		{"tag": "button", "text": map[string]any{"tag": "plain_text", "content": "取消"}, "value": value},
	}
}

package invitations

import (
	"encoding/json/v2"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platformauth"
)

func invitationFeishuMessage(organization, inviter, link string, expiresAt time.Time) ([]byte, error) {
	switch inviter {
	case "Platform administrator":
		inviter = "平台管理员"
	case "Organization administrator":
		inviter = "组织管理员"
	}
	expires := expiresAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04") + "（北京时间）"
	return json.Marshal(platformauth.FeishuCard{
		Schema: "2.0",
		Config: platformauth.FeishuCardConfig{WidthMode: "compact"},
		Header: platformauth.FeishuCardHeader{Title: platformauth.FeishuCardText{Tag: "plain_text", Content: "组织邀请"}, Template: "blue"},
		Body: platformauth.FeishuCardBody{Elements: []platformauth.FeishuCardElement{
			{Tag: "div", Text: new(platformauth.FeishuCardText{Tag: "plain_text", Content: organization, TextSize: "heading-2"})},
			{Tag: "div", Text: new(platformauth.FeishuCardText{Tag: "plain_text", Content: fmt.Sprintf("邀请人：%s\n有效期至：%s", inviter, expires)})},
			{Tag: "button", Type: "primary_filled", Width: "fill", Text: new(platformauth.FeishuCardText{Tag: "plain_text", Content: "查看邀请"}), Behaviors: []platformauth.FeishuCardBehavior{{Type: "open_url", DefaultURL: link}}},
			{Tag: "div", Text: new(platformauth.FeishuCardText{Tag: "plain_text", Content: "使用受邀邮箱登录后确认加入。", TextSize: "notation", TextColor: "grey"})},
		}},
	})
}

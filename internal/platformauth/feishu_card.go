package platformauth

import (
	"encoding/json/v2"
	"fmt"
	"time"
)

type FeishuCard struct {
	Schema string           `json:"schema"`
	Config FeishuCardConfig `json:"config"`
	Header FeishuCardHeader `json:"header"`
	Body   FeishuCardBody   `json:"body"`
}

type FeishuCardConfig struct {
	WidthMode string `json:"width_mode"`
}

type FeishuCardHeader struct {
	Title    FeishuCardText `json:"title"`
	Template string         `json:"template"`
}

type FeishuCardBody struct {
	Elements []FeishuCardElement `json:"elements"`
}

type FeishuCardText struct {
	Tag       string `json:"tag"`
	Content   string `json:"content"`
	TextSize  string `json:"text_size,omitempty"`
	TextAlign string `json:"text_align,omitempty"`
	TextColor string `json:"text_color,omitempty"`
}

type FeishuCardElement struct {
	Tag       string               `json:"tag"`
	Text      *FeishuCardText      `json:"text,omitzero"`
	Content   string               `json:"content,omitempty"`
	TextSize  string               `json:"text_size,omitempty"`
	TextAlign string               `json:"text_align,omitempty"`
	Type      string               `json:"type,omitempty"`
	Width     string               `json:"width,omitempty"`
	Margin    string               `json:"margin,omitempty"`
	Behaviors []FeishuCardBehavior `json:"behaviors,omitempty"`
}

type FeishuCardBehavior struct {
	Type       string `json:"type"`
	DefaultURL string `json:"default_url"`
}

func feishuLoginCodeCard(code string) ([]byte, error) {
	return json.Marshal(FeishuCard{
		Schema: "2.0",
		Config: FeishuCardConfig{WidthMode: "compact"},
		Header: FeishuCardHeader{Title: FeishuCardText{Tag: "plain_text", Content: "登录验证码"}, Template: "blue"},
		Body: FeishuCardBody{Elements: []FeishuCardElement{
			{Tag: "div", Text: new(FeishuCardText{Tag: "plain_text", Content: "本次登录验证码", TextAlign: "center", TextColor: "grey", TextSize: "normal"})},
			{Tag: "markdown", Content: "**" + code + "**", TextAlign: "center", TextSize: "heading-0", Margin: "8px 0px 12px 0px"},
			{Tag: "div", Text: new(FeishuCardText{Tag: "plain_text", Content: fmt.Sprintf("%d 分钟内有效，请勿转发。\n非本人操作请忽略。", emailCodeTTL/time.Minute), TextAlign: "center", TextColor: "grey", TextSize: "notation"})},
		}},
	})
}

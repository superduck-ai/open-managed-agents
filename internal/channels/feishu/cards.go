package feishu

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

func Render(r contracts.Reply) string {
	elements := make([]map[string]any, 0, len(r.Blocks)+2)
	for i := 0; i < len(r.Blocks); {
		block := r.Blocks[i]
		if block.Tool == nil {
			text := block.Text
			if text != "" {
				elements = append(elements, map[string]any{"tag": "markdown", "content": text})
			}
			i++
			continue
		}
		tools := []*contracts.ReplyTool{}
		for i < len(r.Blocks) && r.Blocks[i].Tool != nil {
			tools = append(tools, r.Blocks[i].Tool)
			i++
		}
		elements = append(elements, ToolGroup(tools, r.Ended)...)
	}
	active := !r.Ended && !r.Interrupted
	var stop map[string]any
	if active && r.StopValue != nil {
		stop = button("停止对话", "danger", *r.StopValue, false)
	}
	if active {
		if stop != nil && len(r.Interactions) == 1 && r.Interactions[0].Kind == "oauth" {
			oauth := r.Interactions[0]
			elements = append(elements, oauthElements(r.OAuthOrigin, oauth.Value, oauth.Input, stop)...)
			stop = nil
		} else {
			elements = append(elements, InteractionElements(r)...)
		}
	}
	if r.Thinking && len(r.Interactions) == 0 {
		elements = append(elements, map[string]any{"tag": "markdown", "content": "_🧠 正在思考…_", "text_size": "notation"})
	}
	if len(elements) == 0 {
		elements = append(elements, map[string]any{"tag": "markdown", "content": "_正在处理…_"})
	}
	if stop != nil {
		elements = append(elements, stop)
	}
	status := r.Status
	if active && len(r.Interactions) > 0 {
		status = "等待确认"
		if len(r.Interactions) == 1 && r.Interactions[0].Name == "RequestOAuth" {
			status = "等待授权"
		}
	}
	if status == "" {
		status = "正在处理"
		if r.Thinking {
			status = "思考中"
		}
	}
	color := "blue"
	switch r.Status {
	case "已连接":
		color = "green"
	case "已取消授权", "已结束授权":
		color = "grey"
	case "连接失效", "授权已过期":
		color = "red"
	}
	card := map[string]any{"schema": "2.0", "config": map[string]any{"update_multi": true, "summary": map[string]string{"content": status}}, "header": map[string]any{"title": map[string]string{"tag": "plain_text", "content": cmp.Or(r.Title, status)}, "template": color}, "body": map[string]any{"elements": elements}}
	encoded, _ := json.Marshal(card)
	return string(encoded)
}

func Card(text string, thinking bool) string {
	reply := contracts.Reply{Thinking: thinking}
	if text != "" {
		reply.Blocks = []contracts.ReplyBlock{{Text: text}}
	}
	return Render(reply)
}

func ToolGroup(tools []*contracts.ReplyTool, finished bool) []map[string]any {
	if len(tools) < 3 {
		panels := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			panels = append(panels, toolPanel(tool, false))
		}
		return panels
	}
	prior := tools
	if !finished {
		prior = tools[:len(tools)-1]
	}
	headers := make([]string, 0, len(prior))
	for _, tool := range prior {
		headers = append(headers, "- "+toolTitle(tool))
	}
	title := fmt.Sprintf("☕ **%d 个工具调用**", len(prior))
	if finished {
		title = fmt.Sprintf("☕ **%d 个工具调用（已结束）**", len(prior))
	}
	panels := []map[string]any{collapsible(title, strings.Join(headers, "\n"), false, "blue")}
	if !finished {
		panels = append(panels, toolPanel(tools[len(tools)-1], true))
	}
	return panels
}

func toolTitle(tool *contracts.ReplyTool) string {
	icon := map[string]string{"running": "⏳", "done": "✅", "error": "❌"}[tool.Status]
	var input struct {
		Command     string `json:"command"`
		FilePath    string `json:"file_path"`
		Path        string `json:"path"`
		Query       string `json:"query"`
		Pattern     string `json:"pattern"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal([]byte(tool.Input), &input)
	summary := strings.Join(strings.Fields(cmp.Or(input.Command, input.FilePath, input.Path, input.Query, input.Pattern, input.URL, input.Description)), " ")
	title := icon + " **" + tool.Name + "**"
	if summary != "" {
		title += " — " + Snippet(summary, 80)
	}
	return title
}

func toolPanel(tool *contracts.ReplyTool, expanded bool) map[string]any {
	parts := make([]string, 0, 2)
	if tool.Input != "" {
		parts = append(parts, "**输入**\n```json\n"+Snippet(tool.Input, 600)+"\n```")
	}
	if tool.Output != "" {
		parts = append(parts, "**输出**\n```\n"+Snippet(tool.Output, 1200)+"\n```")
	}
	body := strings.Join(parts, "\n\n")
	if body == "" {
		body = "_无输出_"
	}
	color := "grey"
	if tool.Status == "error" {
		color = "red"
	}
	return collapsible(toolTitle(tool), body, expanded, color)
}

func collapsible(title, body string, expanded bool, color string) map[string]any {
	return map[string]any{"tag": "collapsible_panel", "expanded": expanded, "header": map[string]any{"title": map[string]string{"tag": "markdown", "content": title}, "vertical_align": "center", "icon": map[string]string{"tag": "standard_icon", "token": "down-small-ccm_outlined", "size": "16px 16px"}, "icon_position": "follow_text", "icon_expanded_angle": -180}, "border": map[string]string{"color": color, "corner_radius": "5px"}, "vertical_spacing": "8px", "padding": "8px 8px 8px 8px", "elements": []map[string]string{{"tag": "markdown", "content": body, "text_size": "notation"}}}
}

func Snippet(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
func ApprovalElements(value contracts.ActionValue, name, input string) []map[string]any {
	summary := name
	if summary == "" {
		summary = "工具调用"
	}
	if input != "" {
		summary += "\n" + Snippet(input, 4000)
	}
	allow := value
	allow.Result = "allow"
	deny := value
	deny.Result = "deny"
	return []map[string]any{
		{"tag": "markdown", "content": "**等待批准**\n" + summary},
		button("允许", "primary", allow, false),
		button("拒绝", "danger", deny, false),
	}
}

func QuestionElements(value contracts.ActionValue, questions []contracts.Question) []map[string]any {
	elements := make([]map[string]any, 0, len(questions)+2)
	formElements := make([]map[string]any, 0, len(questions)*3+1)
	for _, question := range questions {
		title := question.Title
		if title == "" {
			title = "请选择"
		}
		lines := []string{"**" + title + "**"}
		for _, option := range question.Options {
			if option.Description != "" {
				lines = append(lines, "- "+option.Label+"："+option.Description)
			}
		}
		formElements = append(formElements, map[string]any{"tag": "markdown", "content": strings.Join(lines, "\n")})
		if len(question.Options) > 0 {
			options := make([]map[string]any, 0, len(question.Options))
			for _, option := range question.Options {
				options = append(options, map[string]any{
					"text":  map[string]any{"tag": "plain_text", "content": option.Label},
					"value": option.Label,
				})
			}
			tag := "select_static"
			placeholder := "请选择"
			if question.MultiSelect {
				tag = "multi_select_static"
				placeholder = "可多选"
			}
			formElements = append(formElements, map[string]any{
				"tag":         tag,
				"name":        question.ID,
				"placeholder": map[string]any{"tag": "plain_text", "content": placeholder},
				"options":     options,
			})
		}
		formElements = append(formElements, map[string]any{
			"tag":         "input",
			"name":        question.ID + "_custom",
			"placeholder": map[string]any{"tag": "plain_text", "content": "其他答案（可选）"},
		})
	}
	submit := value
	submit.Kind = "answer"
	formElements = append(formElements, button("提交", "primary", submit, true))
	elements = append(elements, map[string]any{"tag": "form", "name": "answers_" + value.ToolUseID, "elements": formElements})
	cancel := value
	cancel.Kind = "cancel_question"
	elements = append(elements, button("取消问卷", "default", cancel, false))
	return elements
}

func button(label, style string, value contracts.ActionValue, submit bool) map[string]any {
	spec := map[string]any{
		"tag":       "button",
		"text":      map[string]any{"tag": "plain_text", "content": label},
		"type":      style,
		"behaviors": []map[string]any{{"type": "callback", "value": value}},
	}
	if submit {
		spec["name"] = "submit"
		spec["form_action_type"] = "submit"
	}
	return spec
}

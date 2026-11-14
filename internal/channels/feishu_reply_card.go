package channels

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type channelReplyTool struct {
	id     string
	name   string
	input  string
	output string
	status string
}

type channelReplyBlock struct {
	eventID string
	preview *strings.Builder
	text    string
	tool    *channelReplyTool
}

type channelReply struct {
	interrupted  bool
	thinking     bool
	ended        bool
	status       string
	text         strings.Builder
	blocks       []channelReplyBlock
	stopValue    *channelActionValue
	interactions []omaEvent
}

func (r *channelReply) consume(record db.ChannelRecord, event omaEvent) (string, bool) {
	if r.ended {
		return "", false
	}
	interactionChanged := r.consumeInteraction(event)
	switch event.Type {
	case "user.interrupt":
		r.interrupted = true
		r.thinking = false
		r.status = "正在停止"
		return "", true
	case "session.error":
		r.status = "出错"
		return r.finish(channelExecutionFailedMessage)
	case "session.status_terminated":
		r.status = "已结束"
		return r.finish("会话已结束。")
	case "session.status_idle":
		if !terminalEvent(event) {
			return "", false
		}
		r.status = "已完成"
		if stopReason(event) == "interrupted" || r.interrupted {
			r.status = "已停止"
			return r.finish("已停止本轮任务。")
		}
		if stopReason(event) == "retries_exhausted" {
			r.status = "出错"
			return r.finish(channelExecutionFailedMessage)
		}
		if r.text.Len() == 0 {
			return r.finish("本轮已结束。")
		}
		return r.finish("")
	case "agent.message":
		text := eventText(event)
		if text == "" {
			return "", false
		}
		r.text.WriteString(text + "\n")
		replaced := false
		for index := range r.blocks {
			if event.ID != "" && r.blocks[index].eventID == event.ID {
				r.blocks[index].text = text
				r.blocks[index].preview = nil
				replaced = true
				break
			}
		}
		if !replaced {
			r.blocks = append(r.blocks, channelReplyBlock{eventID: event.ID, text: text})
		}
		r.thinking = false
		r.status = "正在输出"
		return text + "\n", true
	case "agent.thinking":
		if record.IncludeThinking && !r.thinking {
			r.thinking = true
			r.status = "思考中"
			return "", true
		}
	case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
		if !record.IncludeToolCalls {
			return "", interactionChanged
		}
		name := eventToolName(event)
		if name == "" {
			return "", false
		}
		line := "🔧 " + name + "\n"
		r.text.WriteString(line)
		r.blocks = append(r.blocks, channelReplyBlock{tool: &channelReplyTool{id: event.ID, name: name, input: string(event.Input), status: "running"}})
		r.thinking = false
		r.status = "正在调用工具"
		return line, true
	case "agent.tool_result", "agent.mcp_tool_result", "agent.custom_tool_result":
		return "", r.consumeToolResult(event) || interactionChanged
	}
	return "", interactionChanged
}

func (r *channelReply) consumeToolResult(event omaEvent) bool {
	for _, block := range r.blocks {
		if block.tool != nil && block.tool.id == eventToolUseID(event) {
			block.tool.output = eventText(event)
			block.tool.status = "done"
			if event.IsError {
				block.tool.status = "error"
			}
			return true
		}
	}
	return false
}

func (r *channelReply) finish(text string) (string, bool) {
	if r.ended {
		return "", false
	}
	r.ended = true
	r.thinking = false
	if text != "" {
		r.blocks = append(r.blocks, channelReplyBlock{text: text})
		text += "\n"
		r.text.WriteString(text)
	}
	return text, true
}

func (r *channelReply) render() string {
	elements := make([]map[string]any, 0, len(r.blocks)+2)
	for i := 0; i < len(r.blocks); {
		block := r.blocks[i]
		if block.tool == nil {
			text := block.text
			if block.preview != nil {
				text = block.preview.String()
			}
			if text != "" {
				elements = append(elements, map[string]any{"tag": "markdown", "content": text})
			}
			i++
			continue
		}
		tools := []*channelReplyTool{}
		for i < len(r.blocks) && r.blocks[i].tool != nil {
			tools = append(tools, r.blocks[i].tool)
			i++
		}
		elements = append(elements, channelToolGroup(tools, r.ended)...)
	}
	if !r.ended && !r.interrupted {
		elements = append(elements, r.interactionElements()...)
	}
	if r.thinking && len(r.interactions) == 0 {
		elements = append(elements, map[string]any{"tag": "markdown", "content": "_🧠 正在思考…_", "text_size": "notation"})
	}
	if len(elements) == 0 {
		elements = append(elements, map[string]any{"tag": "markdown", "content": "_正在处理…_"})
	}
	if !r.ended && !r.interrupted && r.stopValue != nil {
		elements = append(elements, map[string]any{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "停止对话"}, "type": "danger", "behaviors": []map[string]any{{"type": "callback", "value": r.stopValue}}})
	}
	status := r.status
	if !r.ended && !r.interrupted && len(r.interactions) > 0 {
		status = "等待确认"
	}
	if status == "" {
		status = "正在处理"
		if r.thinking {
			status = "思考中"
		}
	}
	card := map[string]any{"schema": "2.0", "config": map[string]any{"update_multi": true, "summary": map[string]string{"content": status}}, "header": map[string]any{"title": map[string]string{"tag": "plain_text", "content": status}, "template": "blue"}, "body": map[string]any{"elements": elements}}
	encoded, _ := json.Marshal(card)
	return string(encoded)
}

func channelCard(text string, thinking bool) string {
	reply := channelReply{thinking: thinking}
	if text != "" {
		reply.blocks = []channelReplyBlock{{text: text}}
	}
	return reply.render()
}

func channelToolGroup(tools []*channelReplyTool, finished bool) []map[string]any {
	if len(tools) < 3 {
		panels := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			panels = append(panels, channelToolPanel(tool, false))
		}
		return panels
	}
	prior := tools
	if !finished {
		prior = tools[:len(tools)-1]
	}
	headers := make([]string, 0, len(prior))
	for _, tool := range prior {
		headers = append(headers, "- "+channelToolTitle(tool))
	}
	title := fmt.Sprintf("☕ **%d 个工具调用**", len(prior))
	if finished {
		title = fmt.Sprintf("☕ **%d 个工具调用（已结束）**", len(prior))
	}
	panels := []map[string]any{channelCollapsible(title, strings.Join(headers, "\n"), false, "blue")}
	if !finished {
		panels = append(panels, channelToolPanel(tools[len(tools)-1], true))
	}
	return panels
}

func channelToolTitle(tool *channelReplyTool) string {
	icon := map[string]string{"running": "⏳", "done": "✅", "error": "❌"}[tool.status]
	var input struct {
		Command     string `json:"command"`
		FilePath    string `json:"file_path"`
		Path        string `json:"path"`
		Query       string `json:"query"`
		Pattern     string `json:"pattern"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal([]byte(tool.input), &input)
	summary := strings.Join(strings.Fields(cmp.Or(input.Command, input.FilePath, input.Path, input.Query, input.Pattern, input.URL, input.Description)), " ")
	title := icon + " **" + tool.name + "**"
	if summary != "" {
		title += " — " + channelCardSnippet(summary, 80)
	}
	return title
}

func channelToolPanel(tool *channelReplyTool, expanded bool) map[string]any {
	parts := make([]string, 0, 2)
	if tool.input != "" {
		parts = append(parts, "**输入**\n```json\n"+channelCardSnippet(tool.input, 600)+"\n```")
	}
	if tool.output != "" {
		parts = append(parts, "**输出**\n```\n"+channelCardSnippet(tool.output, 1200)+"\n```")
	}
	body := strings.Join(parts, "\n\n")
	if body == "" {
		body = "_无输出_"
	}
	color := "grey"
	if tool.status == "error" {
		color = "red"
	}
	return channelCollapsible(channelToolTitle(tool), body, expanded, color)
}

func channelCollapsible(title, body string, expanded bool, color string) map[string]any {
	return map[string]any{"tag": "collapsible_panel", "expanded": expanded, "header": map[string]any{"title": map[string]string{"tag": "markdown", "content": title}, "vertical_align": "center", "icon": map[string]string{"tag": "standard_icon", "token": "down-small-ccm_outlined", "size": "16px 16px"}, "icon_position": "follow_text", "icon_expanded_angle": -180}, "border": map[string]string{"color": color, "corner_radius": "5px"}, "vertical_spacing": "8px", "padding": "8px 8px 8px 8px", "elements": []map[string]string{{"tag": "markdown", "content": body, "text_size": "notation"}}}
}

func channelCardSnippet(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

package channels

import (
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
	oauthOrigin  string
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
	case "agent.tool_result", "agent.mcp_tool_result", "agent.custom_tool_result", "user.custom_tool_result":
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

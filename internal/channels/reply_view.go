package channels

import "github.com/superduck-ai/open-managed-agents/internal/channels/contracts"

func (r *channelReply) view() contracts.Reply {
	view := contracts.Reply{OAuthOrigin: r.oauthOrigin, Interrupted: r.interrupted, Thinking: r.thinking, Ended: r.ended, Status: r.status, Text: r.text.String(), StopValue: r.stopValue, Blocks: make([]contracts.ReplyBlock, 0, len(r.blocks))}
	for _, block := range r.blocks {
		item := contracts.ReplyBlock{Text: block.text}
		if block.preview != nil {
			item.Text = block.preview.String()
		}
		if block.tool != nil {
			item.Tool = &contracts.ReplyTool{ID: block.tool.id, Name: block.tool.name, Input: block.tool.input, Output: block.tool.output, Status: block.tool.status}
		}
		view.Blocks = append(view.Blocks, item)
	}
	for _, event := range r.interactions {
		value := channelActionValue{}
		if r.stopValue != nil {
			value = *r.stopValue
		}
		value.ToolUseID = event.ID
		value.ThreadID = event.SessionThreadID
		kind := "approval"
		if event.Type == "agent.custom_tool_use" {
			kind = "question"
			if event.Name == "RequestOAuth" {
				kind = "oauth"
			}
		}
		interaction := contracts.Interaction{Kind: kind, Value: value, Name: eventToolName(event), Input: string(event.Input)}
		if kind == "question" {
			interaction.Questions = parseQuestions(event.Input)
		}
		view.Interactions = append(view.Interactions, interaction)
	}
	return view
}

package channels

import "slices"

func (r *channelReply) consumeInteraction(event omaEvent) bool {
	switch event.Type {
	case "agent.tool_use", "agent.mcp_tool_use":
		if event.Permission != "ask" {
			return false
		}
	case "agent.custom_tool_use":
		if !isQuestionTool(event.Name) {
			return false
		}
	case "user.tool_confirmation", "user.custom_tool_result", "agent.tool_result", "agent.mcp_tool_result", "agent.custom_tool_result":
		before := len(r.interactions)
		r.interactions = slices.DeleteFunc(r.interactions, func(request omaEvent) bool { return request.ID == eventToolUseID(event) })
		return before != len(r.interactions)
	default:
		return false
	}
	if event.ID == "" {
		return false
	}
	r.interactions = append(r.interactions, event)
	r.thinking = false
	return true
}

func (r *channelReply) interactionElements() []map[string]any {
	if r.stopValue == nil {
		return nil
	}
	elements := []map[string]any{}
	for _, event := range r.interactions {
		value := *r.stopValue
		value.ToolUseID = event.ID
		value.ThreadID = event.SessionThreadID
		if event.Type == "agent.custom_tool_use" {
			value.Kind = "answer"
			questions := parseQuestions(event.Input)
			if len(questions) == 0 {
				elements = append(elements, map[string]any{"tag": "markdown", "content": "问卷结构暂不支持，请在 OMA 控制台回答。"})
				continue
			}
			elements = append(elements, questionElements(value, questions)...)
			continue
		}
		value.Kind = "confirm"
		elements = append(elements, approvalElements(value, eventToolName(event), string(event.Input))...)
	}
	return elements
}

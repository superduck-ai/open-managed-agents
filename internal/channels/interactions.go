package channels

import "slices"

func (r *channelReply) consumeInteraction(event omaEvent) bool {
	switch event.Type {
	case "agent.tool_use", "agent.mcp_tool_use":
		if event.Permission != "ask" {
			return false
		}
	case "agent.custom_tool_use":
		if !isQuestionTool(event.Name) && event.Name != "RequestOAuth" {
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

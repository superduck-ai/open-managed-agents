package feishu

import "github.com/superduck-ai/open-managed-agents/internal/channels/contracts"

func InteractionElements(reply contracts.Reply) []map[string]any {
	if reply.StopValue == nil {
		return nil
	}
	elements := []map[string]any{}
	for _, interaction := range reply.Interactions {
		value := interaction.Value
		switch interaction.Kind {
		case "oauth":
			elements = append(elements, oauthElements(reply.OAuthOrigin, value, interaction.Input, nil)...)
		case "question":
			value.Kind = "answer"
			if len(interaction.Questions) == 0 {
				elements = append(elements, map[string]any{"tag": "markdown", "content": "问卷结构暂不支持，请在 OMA 控制台回答。"})
				continue
			}
			elements = append(elements, QuestionElements(value, interaction.Questions)...)
		default:
			value.Kind = "confirm"
			elements = append(elements, ApprovalElements(value, interaction.Name, interaction.Input)...)
		}
	}
	return elements
}

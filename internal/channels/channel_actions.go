package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

type channelActionValue struct {
	Kind       string   `json:"kind"`
	SessionID  string   `json:"session_id"`
	ToolUseID  string   `json:"tool_use_id"`
	Result     string   `json:"result,omitempty"`
	QuestionID string   `json:"question_id,omitempty"`
	Selected   []string `json:"selected,omitempty"`
}

func (w *channelWorker) askIfNeeded(ctx context.Context, msg *channeltypes.NormalizedMessage, sessionID string, event omaEvent) error {
	switch event.Type {
	case "agent.tool_use":
		if event.Permission != "ask" || event.ID == "" {
			return nil
		}
		if !w.markAsked("confirm:" + event.ID) {
			return nil
		}
		card := approvalCard(event.ID, sessionID, event.Name, strings.TrimSpace(string(event.Input)))
		_, err := w.channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Card: card})
		return err
	case "agent.custom_tool_use":
		if event.ID == "" || !isQuestionTool(event.Name) {
			return nil
		}
		questions := parseQuestions(event.Input)
		if len(questions) == 0 {
			return nil
		}
		if !w.markAsked("answer:" + event.ID) {
			return nil
		}
		card := questionCard(event.ID, sessionID, questions)
		_, err := w.channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Card: card})
		return err
	default:
		return nil
	}
}

func (w *channelWorker) markAsked(key string) bool {
	w.askedMu.Lock()
	defer w.askedMu.Unlock()
	if _, ok := w.asked[key]; ok {
		return false
	}
	w.asked[key] = struct{}{}
	return true
}

func (w *channelWorker) handleCardAction(ctx context.Context, event *channeltypes.CardActionEvent) error {
	if event == nil {
		return nil
	}
	value, err := decodeActionValue(event.Action.Value)
	if err != nil || value.SessionID == "" || value.ToolUseID == "" {
		return err
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return err
	}
	switch value.Kind {
	case "confirm":
		result := value.Result
		if result != "allow" {
			result = "deny"
		}
		return w.supervisor.oma.sendEvent(ctx, apiKey, value.SessionID, map[string]any{
			"type":        "user.tool_confirmation",
			"tool_use_id": value.ToolUseID,
			"result":      result,
		})
	case "answer":
		answers := map[string]any{"answers": []map[string]any{{
			"id":       value.QuestionID,
			"selected": value.Selected,
		}}}
		content, err := json.Marshal(answers)
		if err != nil {
			return err
		}
		return w.supervisor.oma.sendEvent(ctx, apiKey, value.SessionID, map[string]any{
			"type":               "user.custom_tool_result",
			"custom_tool_use_id": value.ToolUseID,
			"content":            []map[string]string{{"type": "text", "text": string(content)}},
		})
	default:
		return nil
	}
}

func decodeActionValue(raw map[string]any) (channelActionValue, error) {
	if raw == nil {
		return channelActionValue{}, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return channelActionValue{}, err
	}
	var value channelActionValue
	if err := json.Unmarshal(encoded, &value); err != nil {
		return channelActionValue{}, fmt.Errorf("渠道卡片参数无效: %w", err)
	}
	return value, nil
}

type channelQuestion struct {
	ID      string   `json:"id"`
	Title   string   `json:"question"`
	Options []string `json:"-"`
}

func parseQuestions(raw json.RawMessage) []channelQuestion {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		Questions []struct {
			ID      string `json:"id"`
			Title   string `json:"question"`
			Options []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	questions := make([]channelQuestion, 0, len(payload.Questions))
	for _, item := range payload.Questions {
		question := channelQuestion{ID: item.ID, Title: item.Title}
		for _, option := range item.Options {
			if strings.TrimSpace(option.Label) != "" {
				question.Options = append(question.Options, option.Label)
			}
		}
		if question.ID != "" {
			questions = append(questions, question)
		}
	}
	return questions
}

func isQuestionTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "askuserquestion", "askuserquestions":
		return true
	default:
		return false
	}
}

func approvalCard(toolUseID, sessionID, name, input string) string {
	summary := name
	if summary == "" {
		summary = "工具调用"
	}
	if input != "" {
		summary += "\n" + truncateRunes(input, 300)
	}
	return actionCard("需要确认", summary, []cardButton{
		{Label: "允许", Type: "primary", Value: channelActionValue{Kind: "confirm", SessionID: sessionID, ToolUseID: toolUseID, Result: "allow"}},
		{Label: "拒绝", Type: "danger", Value: channelActionValue{Kind: "confirm", SessionID: sessionID, ToolUseID: toolUseID, Result: "deny"}},
	})
}

func questionCard(toolUseID, sessionID string, questions []channelQuestion) string {
	lines := make([]string, 0, len(questions))
	buttons := make([]cardButton, 0, len(questions)*3)
	for _, question := range questions {
		lines = append(lines, "**"+question.Title+"**")
		for _, option := range question.Options {
			buttons = append(buttons, cardButton{
				Label: option,
				Type:  "default",
				Value: channelActionValue{Kind: "answer", SessionID: sessionID, ToolUseID: toolUseID, QuestionID: question.ID, Selected: []string{option}},
			})
		}
		if len(question.Options) == 0 {
			lines = append(lines, "_请在下方直接回复答案，或在 OMA 会话中回答_")
		}
	}
	return actionCard("Agent 提问", strings.Join(lines, "\n"), buttons)
}

type cardButton struct {
	Label string
	Type  string
	Value channelActionValue
}

func actionCard(title, content string, buttons []cardButton) string {
	elements := []map[string]any{{"tag": "markdown", "content": content}}
	if len(buttons) > 0 {
		actions := make([]map[string]any, 0, len(buttons))
		for _, button := range buttons {
			actions = append(actions, map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": button.Label},
				"type": button.Type,
				"behaviors": []map[string]any{{
					"type":  "callback",
					"value": button.Value,
				}},
			})
		}
		elements = append(elements, map[string]any{"tag": "action", "actions": actions})
	}
	card := map[string]any{
		"schema": "2.0",
		"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": title}},
		"body":   map[string]any{"elements": elements},
		"config": map[string]any{"update_multi": true},
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

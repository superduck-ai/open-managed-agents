package channels

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"
	"unicode/utf16"
)

type channelQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type channelQuestion struct {
	ID          string                  `json:"id"`
	Title       string                  `json:"question"`
	Options     []channelQuestionOption `json:"options"`
	MultiSelect bool                    `json:"multi_select"`
}

type channelQuestionAnswer struct {
	ID       string   `json:"id"`
	Selected []string `json:"selected"`
	Custom   string   `json:"custom,omitempty"`
}

func parseQuestions(raw []byte) []channelQuestion {
	var payload struct {
		Questions []channelQuestion `json:"questions"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || len(payload.Questions) == 0 || len(payload.Questions) > 10 {
		return nil
	}
	seen := make(map[string]struct{}, len(payload.Questions))
	for i := range payload.Questions {
		question := &payload.Questions[i]
		question.Title = strings.TrimSpace(question.Title)
		if _, duplicate := seen[question.ID]; question.ID == "" || question.Title == "" || question.Options == nil || duplicate {
			return nil
		}
		seen[question.ID] = struct{}{}
		for j := range question.Options {
			option := &question.Options[j]
			option.Label = strings.TrimSpace(option.Label)
			option.Description = strings.TrimSpace(option.Description)
			if option.Label == "" {
				return nil
			}
		}
	}
	return payload.Questions
}

func buildAnswers(questions []channelQuestion, form map[string]any) ([]channelQuestionAnswer, error) {
	if len(questions) == 0 {
		return nil, invalidQuestionDefinition()
	}
	answers := make([]channelQuestionAnswer, 0, len(questions))
	for index, question := range questions {
		other, _ := form[question.ID+"_custom"].(string)
		custom := strings.TrimSpace(other)
		answer := channelQuestionAnswer{ID: question.ID, Selected: []string{}, Custom: custom}
		if custom != "" {
			if len(utf16.Encode([]rune(other))) > 10000 {
				return nil, questionAnswerTooLong(index + 1)
			}
			answers = append(answers, answer)
			continue
		}
		var values []any
		switch raw := form[question.ID].(type) {
		case nil:
		case string:
			if raw != "" {
				values = []any{raw}
			}
		case []any:
			values = raw
		default:
			return nil, invalidQuestionOption(index + 1)
		}
		if len(values) == 0 || !question.MultiSelect && len(values) != 1 {
			return nil, questionAnswerRequired(index + 1)
		}
		for _, value := range values {
			label, ok := value.(string)
			if !ok || !slices.ContainsFunc(question.Options, func(option channelQuestionOption) bool { return option.Label == label }) {
				return nil, invalidQuestionOption(index + 1)
			}
			if !slices.Contains(answer.Selected, label) {
				answer.Selected = append(answer.Selected, label)
			}
		}
		answers = append(answers, answer)
	}
	return answers, nil
}

func (w *channelWorker) answerQuestion(ctx context.Context, apiKey string, value channelActionValue, form map[string]any, events []omaEvent) error {
	var request omaEvent
	for _, candidate := range events {
		if candidate.ID == value.ToolUseID {
			request = candidate
			break
		}
	}
	if request.Type != "agent.custom_tool_use" || !isQuestionTool(request.Name) {
		return questionInteractionUnavailable()
	}
	text := "用户取消了问卷"
	if value.Kind == "answer" {
		answers, err := buildAnswers(parseQuestions(request.Input), form)
		if err != nil {
			return err
		}
		content, err := json.Marshal(map[string]any{"answers": answers})
		if err != nil {
			return err
		}
		text = string(content)
	}
	resultEvent := map[string]any{
		"type":               "user.custom_tool_result",
		"custom_tool_use_id": value.ToolUseID,
		"is_error":           value.Kind == "cancel_question",
		"content":            []map[string]string{{"type": "text", "text": text}},
	}
	if request.SessionThreadID != "" {
		resultEvent["session_thread_id"] = request.SessionThreadID
	}
	return w.supervisor.oma.sendEvent(ctx, apiKey, value.SessionID, resultEvent, value.requestKey)
}

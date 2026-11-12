package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

type channelActionValue struct {
	Kind      string   `json:"kind"`
	ChannelID string   `json:"channel_id"`
	ScopeKey  string   `json:"scope_key"`
	SessionID string   `json:"session_id"`
	ToolUseID string   `json:"tool_use_id"`
	Result    string   `json:"result,omitempty"`
	Question  string   `json:"question_id,omitempty"`
	Selected  []string `json:"selected,omitempty"`
	Multi     []string `json:"multi,omitempty"`
	OpenID    string   `json:"open_id"`
	ChatType  string   `json:"chat_type,omitempty"`
	ThreadID  string   `json:"session_thread_id,omitempty"`
}

type askedCard struct {
	attempts int
	nextTry  time.Time
	sent     bool
	failed   bool
}

func (w *channelWorker) askIfNeeded(ctx context.Context, msg *channeltypes.NormalizedMessage, task *channelReplyTask, event omaEvent, answered map[string]struct{}) error {
	if event.ID == "" {
		return nil
	}
	answeredID := eventToolUseID(event)
	if answeredID == "" {
		answeredID = event.ID
	}
	if _, done := answered[answeredID]; done {
		return nil
	}
	var key, card string
	switch event.Type {
	case "agent.tool_use", "agent.mcp_tool_use":
		if event.Permission != "ask" {
			return nil
		}
		key = "confirm:" + task.sessionID + ":" + event.ID
		card = approvalCard(w.cardValue(task, event.ID, "confirm", msg.UserID, event.SessionThreadID, msg.ChatType), eventToolName(event), strings.TrimSpace(string(event.Input)))
	case "agent.custom_tool_use":
		if !isQuestionTool(event.Name) {
			return nil
		}
		questions := parseQuestions(event.Input)
		if len(questions) == 0 {
			return nil
		}
		key = "answer:" + task.sessionID + ":" + event.ID
		card = questionCard(w.cardValue(task, event.ID, "answer", msg.UserID, event.SessionThreadID, msg.ChatType), questions)
	default:
		return nil
	}
	if !w.beginAsk(key) {
		return nil
	}
	if _, err := w.channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Card: card}); err != nil {
		if w.askFailed(key) {
			w.notify(ctx, msg, "审批卡片发送失败，请在 OMA 会话中处理，或发送 /new 后重试。")
		}
		return err
	}
	w.askSent(key)
	return nil
}

func (w *channelWorker) beginAsk(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	state := w.asked[key]
	if state == nil {
		state = &askedCard{}
		w.asked[key] = state
	}
	if state.sent || state.failed || time.Now().Before(state.nextTry) {
		return false
	}
	state.attempts++
	state.nextTry = time.Now().Add(time.Duration(1<<uint(state.attempts-1)) * channelAskRetryBase)
	return true
}

func (w *channelWorker) askSent(key string) {
	w.mu.Lock()
	if state := w.asked[key]; state != nil {
		state.sent = true
	}
	w.mu.Unlock()
}

func (w *channelWorker) askFailed(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	state := w.asked[key]
	if state == nil {
		return false
	}
	if state.attempts >= channelAskAttempts {
		state.failed = true
		return true
	}
	return false
}

func (w *channelWorker) cardValue(task *channelReplyTask, toolUseID, kind, openID, threadID, chatType string) channelActionValue {
	return channelActionValue{
		Kind:      kind,
		ChannelID: w.record.ExternalID,
		ScopeKey:  task.scopeKey,
		SessionID: task.sessionID,
		ToolUseID: toolUseID,
		OpenID:    openID,
		ChatType:  chatType,
		ThreadID:  threadID,
	}
}

func answeredToolIDs(events []omaEvent) map[string]struct{} {
	answered := make(map[string]struct{})
	for _, event := range events {
		switch event.Type {
		case "user.tool_confirmation":
			if id := eventToolUseID(event); id != "" {
				answered[id] = struct{}{}
			}
		case "user.custom_tool_result":
			if id := eventToolUseID(event); id != "" {
				answered[id] = struct{}{}
			}
		}
	}
	return answered
}

func eventToolUseID(event omaEvent) string {
	for _, id := range []string{event.ToolUseID, event.MCPToolUseID, event.CustomToolUseID} {
		if id != "" {
			return id
		}
	}
	return ""
}

func (w *channelWorker) handleCardAction(ctx context.Context, event *channeltypes.CardActionEvent) error {
	if event == nil {
		return nil
	}
	value, err := decodeActionValue(event.Action.Value)
	if err != nil || value.SessionID == "" || value.ToolUseID == "" {
		return err
	}
	if value.ChannelID != w.record.ExternalID {
		return fmt.Errorf("渠道卡片不属于当前渠道")
	}
	operator := ""
	if event.Operator.OpenID != "" {
		operator = event.Operator.OpenID
	}
	if value.OpenID != "" && operator != "" && value.OpenID != operator {
		return fmt.Errorf("只有发起人本人可以操作该卡片")
	}
	if event.ChatID != "" && !scopeMatchesChat(value.ScopeKey, event.ChatID) {
		return fmt.Errorf("渠道卡片不属于当前会话")
	}
	allowed, err := w.cardAuthorized(ctx, value, operator, event.ChatID)
	if err != nil {
		return err
	}
	if !allowed {
		w.notifyChat(ctx, event.ChatID, "当前会话未获授权，操作已拒绝。")
		return fmt.Errorf("当前会话未获授权")
	}
	if w.currentSession(ctx, value.ScopeKey) != value.SessionID {
		return fmt.Errorf("该操作已失效，请重新发起")
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return err
	}
	events, listErr := w.supervisor.oma.listEvents(ctx, apiKey, value.SessionID)
	if listErr == nil {
		if _, done := answeredToolIDs(events)[value.ToolUseID]; done {
			return nil
		}
	}
	switch value.Kind {
	case "confirm":
		result := "deny"
		if value.Result == "allow" {
			result = "allow"
		}
		confirmation := map[string]any{
			"type":        "user.tool_confirmation",
			"tool_use_id": value.ToolUseID,
			"result":      result,
		}
		if value.ThreadID != "" {
			confirmation["session_thread_id"] = value.ThreadID
		}
		if err := w.supervisor.oma.sendEvent(ctx, apiKey, value.SessionID, confirmation); err != nil {
			return err
		}
		w.resumeListener(value, event)
		return nil
	case "answer":
		answers := buildAnswers(value, event.Action.FormValue)
		content, err := json.Marshal(map[string]any{"answers": answers})
		if err != nil {
			return err
		}
		resultEvent := map[string]any{
			"type":               "user.custom_tool_result",
			"custom_tool_use_id": value.ToolUseID,
			"content":            []map[string]string{{"type": "text", "text": string(content)}},
		}
		if value.ThreadID != "" {
			resultEvent["session_thread_id"] = value.ThreadID
		}
		if err := w.supervisor.oma.sendEvent(ctx, apiKey, value.SessionID, resultEvent); err != nil {
			return err
		}
		w.resumeListener(value, event)
		return nil
	default:
		return nil
	}
}

func (w *channelWorker) resumeListener(value channelActionValue, event *channeltypes.CardActionEvent) {
	if event.ChatID == "" || value.ScopeKey == "" || value.SessionID == "" {
		return
	}
	task := w.startListener(value.ScopeKey, value.SessionID)
	if task == nil {
		return
	}
	apiKey, err := w.supervisor.apiKey(task.ctx, w.record)
	if err != nil {
		w.finishReply(task)
		return
	}
	msg := &channeltypes.NormalizedMessage{ChatID: event.ChatID, MessageID: event.MessageID, ChatType: value.ChatType}
	go w.listen(task, msg, apiKey)
}

func (w *channelWorker) notifyChat(ctx context.Context, chatID, text string) {
	if chatID == "" {
		return
	}
	if _, err := w.channel.Send(ctx, &channeltypes.SendInput{ChatID: chatID, Text: text}); err != nil {
		w.supervisor.logger.DebugContext(ctx, "send channel notice", "channel_id", w.record.ExternalID, "error", err)
	}
}

func scopeMatchesChat(scopeKey, chatID string) bool {
	if scopeKey == "chat:"+chatID {
		return true
	}
	return strings.HasPrefix(scopeKey, "thread:"+chatID+":")
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
	ID          string
	Title       string
	Options     []string
	MultiSelect bool
}

func parseQuestions(raw json.RawMessage) []channelQuestion {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		Questions []struct {
			ID          string `json:"id"`
			Title       string `json:"question"`
			MultiSelect bool   `json:"multi_select"`
			Options     []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	questions := make([]channelQuestion, 0, len(payload.Questions))
	for _, item := range payload.Questions {
		question := channelQuestion{ID: item.ID, Title: item.Title, MultiSelect: item.MultiSelect}
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

func buildAnswers(value channelActionValue, form map[string]any) []map[string]any {
	if len(form) == 0 {
		if len(value.Selected) == 0 && value.Question == "" {
			return []map[string]any{}
		}
		return []map[string]any{{"id": value.Question, "selected": value.Selected}}
	}
	multi := make(map[string]struct{}, len(value.Multi))
	for _, id := range value.Multi {
		multi[id] = struct{}{}
	}
	answers := make([]map[string]any, 0, len(form))
	for key, raw := range form {
		if strings.HasSuffix(key, "_custom") {
			continue
		}
		selected := formStrings(raw)
		custom := strings.TrimSpace(formString(form[key+"_custom"]))
		if _, ok := multi[key]; !ok {
			if custom != "" {
				selected = []string{}
			} else if len(selected) > 1 {
				selected = selected[:1]
			}
		}
		answer := map[string]any{"id": key, "selected": selected}
		if custom != "" {
			answer["custom"] = custom
		}
		answers = append(answers, answer)
	}
	for key, raw := range form {
		if !strings.HasSuffix(key, "_custom") {
			continue
		}
		if _, ok := form[strings.TrimSuffix(key, "_custom")]; ok {
			continue
		}
		custom := strings.TrimSpace(formString(raw))
		if custom == "" {
			continue
		}
		answers = append(answers, map[string]any{"id": strings.TrimSuffix(key, "_custom"), "selected": []string{}, "custom": custom})
	}
	return answers
}

func formStrings(raw any) []string {
	switch value := raw.(type) {
	case string:
		if value == "" {
			return []string{}
		}
		return []string{value}
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return []string{}
	}
}

func formString(raw any) string {
	if text, ok := raw.(string); ok {
		return text
	}
	return ""
}

func approvalCard(value channelActionValue, name, input string) string {
	summary := name
	if summary == "" {
		summary = "工具调用"
	}
	if input != "" {
		summary += "\n" + truncateRunes(input, 300)
	}
	allow := value
	allow.Result = "allow"
	deny := value
	deny.Result = "deny"
	return actionCard("需要确认", summary, []map[string]any{
		button("允许", "primary", allow, false),
		button("拒绝", "danger", deny, false),
	})
}

func questionCard(value channelActionValue, questions []channelQuestion) string {
	elements := make([]map[string]any, 0, len(questions)+2)
	formElements := make([]map[string]any, 0, len(questions)*3+1)
	for _, question := range questions {
		title := question.Title
		if title == "" {
			title = "请选择"
		}
		formElements = append(formElements, map[string]any{"tag": "markdown", "content": "**" + title + "**"})
		if len(question.Options) > 0 {
			options := make([]map[string]any, 0, len(question.Options))
			for _, option := range question.Options {
				options = append(options, map[string]any{
					"text":  map[string]any{"tag": "plain_text", "content": option},
					"value": option,
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
	submit.Multi = make([]string, 0, len(questions))
	for _, question := range questions {
		if question.MultiSelect {
			submit.Multi = append(submit.Multi, question.ID)
		}
	}
	formElements = append(formElements, button("提交", "primary", submit, true))
	elements = append(elements, map[string]any{"tag": "form", "name": "answers", "elements": formElements})
	card := map[string]any{
		"schema": "2.0",
		"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": "Agent 提问"}},
		"body":   map[string]any{"elements": elements},
		"config": map[string]any{"update_multi": true},
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func actionCard(title, content string, actions []map[string]any) string {
	card := map[string]any{
		"schema": "2.0",
		"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": title}},
		"body": map[string]any{"elements": []map[string]any{
			{"tag": "markdown", "content": content},
			{"tag": "action", "actions": actions},
		}},
		"config": map[string]any{"update_multi": true},
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func button(label, style string, value channelActionValue, submit bool) map[string]any {
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

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

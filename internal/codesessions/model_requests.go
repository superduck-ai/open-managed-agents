package codesessions

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

// ModelRequest owns one proxy attempt. Each worker retry gets a new span.
type ModelRequest struct {
	calculator           *billing.Calculator
	SuppressPublicOutput bool
	StartID              string
	CodeSessionID        string
	ThreadID             string
	Model                string
	codeSession          db.CodeSession
	toolThreadID         string
	harnessLease         *db.HarnessTurnLease
	harnessPreview       harnessModelPreview
}

// ModelRequestUsage is cumulative within one provider response, never a turn total.
type ModelRequestUsage struct {
	ServerToolUse            *ModelRequestServerToolUsage `json:"server_tool_use,omitzero"`
	CacheCreation            *ModelRequestCacheUsage      `json:"cache_creation,omitzero"`
	InputTokens              *int64                       `json:"input_tokens,omitzero"`
	OutputTokens             *int64                       `json:"output_tokens,omitzero"`
	CacheCreationInputTokens *int64                       `json:"cache_creation_input_tokens,omitzero"`
	CacheReadInputTokens     *int64                       `json:"cache_read_input_tokens,omitzero"`
}

type ModelRequestServerToolUsage struct {
	WebSearchRequests *int64 `json:"web_search_requests,omitzero"`
	WebFetchRequests  *int64 `json:"web_fetch_requests,omitzero"`
}

type ModelRequestCacheUsage struct {
	Ephemeral5mInputTokens *int64 `json:"ephemeral_5m_input_tokens,omitzero"`
	Ephemeral1hInputTokens *int64 `json:"ephemeral_1h_input_tokens,omitzero"`
}

type ModelRequestResult struct {
	Messages          []ModelRequestMessage
	ToolUseIDs        []string
	ToolUses          []ModelRequestToolUse
	EndedAt           time.Time
	Usage             ModelRequestUsage
	EventIDs          []string
	UpstreamRequestID string
	ErrorType         string
	harnessNext       HarnessModelNext
}

type ModelRequestToolUse struct {
	ID            string
	Name          string
	Input         json.RawMessage
	InputRejected bool
}

// ModelRequestMessage is a completed public message observed at the proxy.
// Publishing it with the end avoids depending on the worker's later echo.
type ModelRequestMessage struct {
	ID      string                `json:"id"`
	Type    string                `json:"type"`
	Content []ModelRequestContent `json:"content,omitempty"`
}

type ModelRequestContent struct {
	Type string  `json:"type"`
	Text *string `json:"text,omitempty"`
}

type modelRequestEvent struct {
	PriceSnapshot     *billing.ModelPrice `json:"_model_price,omitzero"`
	Billing           map[string]string   `json:"billing,omitempty"`
	IsError           *bool               `json:"is_error,omitzero"`
	ToolUseIDs        []string            `json:"tool_use_ids,omitempty"`
	ID                string              `json:"id"`
	Type              string              `json:"type"`
	OwnerThreadID     string              `json:"_owner_session_thread_id,omitempty"`
	Model             string              `json:"model"`
	RequestID         string              `json:"request_id"`
	CreatedAt         time.Time           `json:"created_at"`
	ProcessedAt       time.Time           `json:"processed_at"`
	StartID           string              `json:"model_request_start_id,omitempty"`
	Usage             *ModelRequestUsage  `json:"model_usage,omitzero"`
	EventIDs          []string            `json:"event_ids,omitempty"`
	UpstreamRequestID string              `json:"upstream_request_id,omitempty"`
	Error             *modelRequestError  `json:"error,omitzero"`
}

type modelRequestError struct {
	Type string `json:"type"`
}

func (s *Service) BeginModelRequest(ctx context.Context, workspaceID, sessionID, codeSessionID, agentID, model string) (*ModelRequest, error) {
	return s.beginModelRequest(ctx, workspaceID, sessionID, codeSessionID, agentID, model, nil)
}

func (s *Service) BeginHarnessModelRequest(ctx context.Context, lease db.HarnessTurnLease, sessionID, codeSessionID, model string) (*ModelRequest, error) {
	return s.beginModelRequest(ctx, lease.Ref.WorkspaceUUID, sessionID, codeSessionID, "", model, &lease)
}

func (s *Service) beginModelRequest(ctx context.Context, workspaceID, sessionID, codeSessionID, agentID, model string, lease *db.HarnessTurnLease) (*ModelRequest, error) {
	codeSession, err := s.db.GetCodeSessionBySessionExternalID(ctx, workspaceID, sessionID)
	if err != nil {
		return nil, err
	}
	if codeSession.ExternalID != codeSessionID || codeSession.Status != "active" || s.sink == nil {
		return nil, db.ErrInvalidState
	}
	request, err := s.modelRequest(ctx, codeSession, agentID, model, lease)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	err = s.publishModelRequestEvent(ctx, request, modelRequestEvent{ID: request.StartID, Type: "span.model_request_start", CreatedAt: now, ProcessedAt: now}, nil, nil)
	if err != nil {
		return nil, err
	}
	return request, nil
}

func (s *Service) modelRequest(ctx context.Context, codeSession db.CodeSession, agentID, model string, lease *db.HarnessTurnLease) (*ModelRequest, error) {
	if lease != nil && (codeSession.UUID != lease.CodeSessionUUID || codeSession.CurrentWorkerEpoch != lease.Epoch ||
		codeSession.OrganizationUUID != lease.Ref.OrganizationUUID || codeSession.SessionUUID != lease.Ref.SessionUUID) {
		return nil, db.ErrWorkerEpochMismatch
	}
	threadID, child, err := s.resolveModelRequestThread(ctx, codeSession, agentID, lease)
	if err != nil {
		return nil, err
	}
	startID, err := modelRequestStartID(codeSession.ExternalID, lease)
	if err != nil {
		return nil, err
	}
	calculator, err := s.billing.Scope(ctx, codeSession.OrganizationUUID, codeSession.WorkspaceUUID, model)
	if err != nil {
		return nil, err
	}
	request := &ModelRequest{calculator: calculator, StartID: startID, CodeSessionID: codeSession.ExternalID, ThreadID: threadID, Model: model, codeSession: codeSession, harnessLease: lease}
	if agentID != "" || child {
		request.toolThreadID = threadID
	}
	return request, nil
}

func (r *ModelRequest) IsHarness() bool {
	return r.harnessLease != nil
}

func (s *Service) resolveModelRequestThread(ctx context.Context, code db.CodeSession, agentID string, lease *db.HarnessTurnLease) (string, bool, error) {
	if lease != nil {
		thread, err := s.db.HarnessSubagent(ctx, *lease)
		if err != nil {
			return "", false, err
		}
		if thread != nil {
			return thread.ExternalID, true, nil
		}
	}
	id, err := s.modelRequestThread(ctx, code, agentID)
	return id, false, err
}

func modelRequestStartID(codeSessionID string, lease *db.HarnessTurnLease) (string, error) {
	if lease == nil {
		return ids.New("sevt_")
	}
	return maevents.HarnessModelRequestID(codeSessionID, lease.Ref.UUID, lease.Epoch), nil
}

func (s *Service) StartModelThinking(ctx context.Context, request *ModelRequest, messageID string, index int) error {
	if request.SuppressPublicOutput || usesBrainClaimTransport(request.codeSession) {
		return nil
	}
	if request.harnessLease != nil {
		s.publishHarnessThinking(ctx, request, messageID, index)
		return nil
	}
	message := ModelRequestMessage{ID: maevents.StableAssistantEventID(request.CodeSessionID, messageID, index, "agent.thinking"), Type: "agent.thinking"}
	preview := modelPreviewStart{Type: "event_start", Event: modelPreviewTarget{Type: message.Type, ID: message.ID}, ThreadID: request.toolThreadID}
	raw, err := jsonv2.Marshal(preview)
	if err != nil {
		return err
	}
	s.publishWorkerStreamPayload(ctx, CodeSessionStreamRoute{
		CodeSessionID: request.CodeSessionID, WorkspaceUUID: request.codeSession.WorkspaceUUID,
		SessionExternalID: request.codeSession.SessionExternalID,
	}, request.codeSession.CurrentWorkerEpoch, raw)
	return nil
}

func modelMessagePayload(request *ModelRequest, message ModelRequestMessage, processedAt time.Time) (json.RawMessage, error) {
	if message.Type == "agent.thinking" {
		message.Content = nil
	}
	return jsonv2.Marshal(struct {
		ModelRequestMessage
		OwnerThreadID string    `json:"_owner_session_thread_id"`
		StartID       string    `json:"model_request_start_id"`
		ProcessedAt   time.Time `json:"processed_at"`
	}{ModelRequestMessage: message, OwnerThreadID: request.ThreadID, StartID: request.StartID, ProcessedAt: processedAt})
}

func (s *Service) EndModelRequest(ctx context.Context, request *ModelRequest, result ModelRequestResult) error {
	var completion *harness.Transition
	if request.harnessLease != nil {
		if result.ErrorType == "" && len(result.ToolUses) > 0 {
			tools, err := s.db.HarnessExecutionToolCount(ctx, *request.harnessLease)
			if err != nil {
				return err
			}
			if tools >= db.HarnessMaxToolCalls {
				result.ErrorType = "invalid_response"
			}
		}
		if result.ErrorType == "" && len(result.ToolUses) == 1 && result.ToolUses[0].Name == sessioncontract.HarnessAskUserQuestion {
			policies, err := s.currentRuntimeToolsets(ctx, request.codeSession, "")
			if err != nil {
				return err
			}
			if resolveAgentToolPermission(policies, "ask_user_question") == resolvedToolPermissionDeny {
				result.ErrorType = "invalid_response"
			}
		}
		if request.toolThreadID != "" && slices.ContainsFunc(result.ToolUses, func(tool ModelRequestToolUse) bool {
			return isHarnessDelegationTool(tool.Name) || tool.Name == sessioncontract.HarnessAskUserQuestion
		}) {
			result.ErrorType = "invalid_response"
		}
		result = harnessTextResult(request, result)
		if result.ErrorType == "" {
			transition, err := harnessModelTransition(request, result)
			if err != nil {
				return err
			}
			completion = &transition
		}
	}
	if request.SuppressPublicOutput {
		result.Messages = nil
		result.EventIDs = nil
		result.ToolUses = nil
		result.ToolUseIDs = nil
	}
	processedAt := request.harnessPreview.processedAt(result.EndedAt)
	event := modelRequestEvent{ID: request.StartID + "_end", Type: "span.model_request_end", StartID: request.StartID,
		CreatedAt: result.EndedAt, ProcessedAt: processedAt, IsError: new(result.ErrorType != "" && result.ErrorType != "observation_limit"), Usage: &result.Usage, EventIDs: result.EventIDs, ToolUseIDs: result.ToolUseIDs, UpstreamRequestID: result.UpstreamRequestID}
	if result.ErrorType != "" {
		event.Error = &modelRequestError{Type: result.ErrorType}
	}
	event.Billing = modelRequestBilling(request.calculator, request.Model, result.Usage)
	event.PriceSnapshot = request.calculator.PriceSnapshot(request.Model)
	tools, err := s.modelToolUsePayloads(ctx, request, modelRequestPublicTools(result), processedAt)
	if err != nil {
		return err
	}
	payloads := make([]json.RawMessage, 0, len(result.Messages)+len(tools)+1)
	if result.ErrorType == "" {
		for _, message := range result.Messages {
			if request.harnessPreview.thinkingCompleted[message.ID] {
				continue
			}
			payload, err := modelMessagePayload(request, message, processedAt)
			if err != nil {
				return err
			}
			payloads = append(payloads, payload)
		}
	}
	payloads = append(payloads, tools...)
	return s.publishModelRequestEvent(ctx, request, event, payloads, completion)
}

func modelRequestPublicTools(result ModelRequestResult) []ModelRequestToolUse {
	if result.ErrorType == "" {
		return result.ToolUses
	}
	if result.ErrorType != "" && result.ErrorType != "invalid_response" {
		return nil
	}
	var rejected []ModelRequestToolUse
	for _, tool := range result.ToolUses {
		if tool.InputRejected {
			rejected = append(rejected, tool)
		}
	}
	return rejected
}

func (s *Service) publishModelRequestEvent(ctx context.Context, request *ModelRequest, event modelRequestEvent, preceding []json.RawMessage, completion *harness.Transition) error {
	event.OwnerThreadID, event.Model = request.ThreadID, request.Model
	event.RequestID = request.StartID
	payload, err := jsonv2.Marshal(event)
	if err != nil {
		return err
	}
	if request.harnessLease != nil {
		sink, ok := s.sink.(harnessModelEventSink)
		if !ok {
			return db.ErrInvalidState
		}
		phase := db.HarnessModelStart
		if event.Type == "span.model_request_end" {
			phase = db.HarnessModelEnd
			if event.Error != nil {
				phase = db.HarnessModelError
			}
		}
		return sink.PublishHarnessModelEvents(ctx, request.codeSession, *request.harnessLease, phase, append(preceding, payload), completion)
	}
	return s.sink.PublishCodeSessionEvents(ctx, request.codeSession, append(preceding, payload))
}

type harnessModelEventSink interface {
	PublishHarnessModelEvents(context.Context, db.CodeSession, db.HarnessTurnLease, db.HarnessModelPhase, []json.RawMessage, *harness.Transition) error
}

func (s *Service) modelRequestThread(ctx context.Context, codeSession db.CodeSession, agentID string) (string, error) {
	if agentID == "" {
		primary, found, err := s.db.GetPrimarySessionThread(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
		if err != nil {
			return "", err
		}
		if !found {
			return "", db.ErrNotFound
		}
		return primary.ExternalID, nil
	}
	// CCR task_started and HTTP requests travel independently. Wait before dispatch,
	// so a delayed task mapping never sends a child request into the primary lane.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		mappings, err := s.subagentThreadMappings(ctx, codeSession)
		if err != nil {
			return "", err
		}
		if threadID := mappings[agentID]; threadID != "" {
			return threadID, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("resolve model request thread: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// The task registry is already persisted as thread_created events. Page it rather
// than maintaining a second registry or assuming at most 500 tasks per session.
func (s *Service) subagentThreadMappings(ctx context.Context, codeSession db.CodeSession) (map[string]string, error) {
	params := db.ListSessionEventsPageParams{WorkspaceUUID: codeSession.WorkspaceUUID, SessionExternalID: codeSession.SessionExternalID,
		PrimaryOnly: true, Limit: 500, Order: "asc", Types: []string{"session.thread_created"}}
	mappings := make(map[string]string)
	for {
		events, more, err := s.eventPayloads.ListSessionEventsPage(ctx, params)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			var task subagentLifecycle
			if err := jsonv2.Unmarshal(event.Payload, &task); err != nil {
				return nil, fmt.Errorf("decode thread mapping: %w", err)
			}
			task.addMappings(mappings)
		}
		if !more {
			return mappings, nil
		}
		if len(events) == 0 {
			return nil, errors.New("empty thread mapping page")
		}
		params.Cursor = &db.SessionEventPageCursor{ExternalID: events[len(events)-1].ExternalID}
	}
}

func modelRequestBilling(calculator *billing.Calculator, model string, usage ModelRequestUsage) map[string]string {
	counters := billing.TokenUsage{}
	for _, field := range []struct{ target, value *int64 }{
		{&counters.InputTokens, usage.InputTokens}, {&counters.OutputTokens, usage.OutputTokens},
		{&counters.CacheReadInputTokens, usage.CacheReadInputTokens}, {&counters.CacheCreationInputTokens, usage.CacheCreationInputTokens},
	} {
		if field.value != nil {
			*field.target = *field.value
		}
	}
	if usage.CacheCreation != nil {
		counters.CacheWrite5mTokens = billing.UsageCount(usage.CacheCreation.Ephemeral5mInputTokens)
		counters.CacheWrite1hTokens = billing.UsageCount(usage.CacheCreation.Ephemeral1hInputTokens)
		if usage.CacheCreationInputTokens == nil {
			counters.CacheCreationInputTokens = counters.CacheWrite5mTokens + counters.CacheWrite1hTokens
		}
	}
	cents, priced := calculator.BudgetRequestCents(model, counters)
	if !priced {
		return nil
	}
	return map[string]string{"list_cost": strconv.FormatInt(cents, 10), "currency": "USD"}
}

func harnessAssistantMessage(result ModelRequestResult) harness.AssistantMessage {
	var completion harness.AssistantMessage
	separator := "\n"
	if len(result.ToolUses) == 1 {
		call := result.ToolUses[0]
		completion.Call = harness.ToolCall{ID: harness.ToolCallID(call.ID), Name: call.Name, Arguments: string(call.Input)}
		separator = ""
	}
	var text []string
	for _, message := range result.Messages {
		for _, block := range message.Content {
			if block.Text != nil {
				text = append(text, *block.Text)
			}
		}
	}
	completion.Text = strings.Join(text, separator)
	return completion
}

func HarnessModelWork(subagent bool) harness.ModelWork {
	work := harness.ModelWork{AutoExecuteTools: true}
	if !subagent {
		work.UserChoiceTool = sessioncontract.HarnessAskUserQuestion
	}
	return work
}

func harnessModelTransition(request *ModelRequest, result ModelRequestResult) (harness.Transition, error) {
	message := harnessAssistantMessage(result)
	transition := harness.Transition{Messages: []harness.Message{message}}
	call := message.Call
	switch result.harnessNext {
	case HarnessModelDone:
		if call == (harness.ToolCall{}) && message.Text != "" {
			return transition, nil
		}
	case HarnessModelTool:
		if call.ID != "" && call.Name != "" && call.Name != sessioncontract.HarnessAskUserQuestion {
			transition.Pending = harness.ToolWork{Call: call}
			return transition, nil
		}
	case HarnessModelUserChoice:
		if call.ID != "" && request.toolThreadID == "" && call.Name == sessioncontract.HarnessAskUserQuestion {
			transition.Pending = harness.UserChoiceWork{Call: call}
			return transition, nil
		}
	}
	return harness.Transition{}, fmt.Errorf("%w: model action does not match the completion", ErrProtocol)
}

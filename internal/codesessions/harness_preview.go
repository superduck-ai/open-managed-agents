package codesessions

import (
	"context"
	"encoding/json"
	"slices"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

type harnessModelPreview struct {
	textBlocks map[string]int
	thinking   map[string]struct{}
	eventIDs   []string
	sequence   uint64
	stopped    bool
	finished   bool
}

type modelPreviewTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type modelPreviewStart struct {
	Type     string             `json:"type"`
	Event    modelPreviewTarget `json:"event"`
	ThreadID string             `json:"session_thread_id,omitempty"`
}

type modelPreviewDelta struct {
	ThreadID string                   `json:"session_thread_id,omitempty"`
	Type     string                   `json:"type"`
	EventID  string                   `json:"event_id"`
	Delta    modelPreviewContentDelta `json:"delta"`
}

type modelPreviewContentDelta struct {
	Type    string              `json:"type"`
	Index   int                 `json:"index"`
	Content ModelRequestContent `json:"content"`
}

func (r *ModelRequest) harnessMessageID() string {
	return stablePublicEventID(r.CodeSessionID, r.StartID+":message")
}

func (r *ModelRequest) harnessThinkingID(blockID string) string {
	return stablePublicEventID(r.CodeSessionID, r.StartID+":thinking:"+blockID)
}

func (s *Service) PublishHarnessModelText(ctx context.Context, request *ModelRequest, messageID string, index int, text string) {
	if request.harnessLease == nil || request.SuppressPublicOutput || s.sink == nil || text == "" {
		return
	}
	preview := &request.harnessPreview
	if preview.finished {
		return
	}
	blockID := maevents.StableAssistantEventID(request.CodeSessionID, messageID, index, "agent.message")
	position, exists := preview.textBlocks[blockID]
	if !exists {
		if preview.textBlocks == nil {
			preview.textBlocks = make(map[string]int)
		}
		position = len(preview.textBlocks)
		preview.textBlocks[blockID] = position
	}
	if preview.stopped {
		return
	}
	eventID := request.harnessMessageID()
	if preview.sequence == 0 {
		preview.sequence = 1
		preview.eventIDs = append(preview.eventIDs, eventID)
		start := modelPreviewStart{Type: "event_start", Event: modelPreviewTarget{Type: "agent.message", ID: eventID}, ThreadID: request.toolThreadID}
		if !s.publishHarnessPreview(ctx, request, preview.sequence, start) {
			preview.stopped = true
			return
		}
	}
	preview.sequence++
	delta := modelPreviewDelta{
		Type: "event_delta", EventID: eventID, ThreadID: request.toolThreadID,
		Delta: modelPreviewContentDelta{Type: "content_delta", Index: position, Content: ModelRequestContent{Type: "text", Text: &text}},
	}
	preview.stopped = !s.publishHarnessPreview(ctx, request, preview.sequence, delta)
}

func (s *Service) publishHarnessThinking(ctx context.Context, request *ModelRequest, messageID string, index int) {
	if s.sink == nil {
		return
	}
	preview := &request.harnessPreview
	if preview.finished {
		return
	}
	blockID := maevents.StableAssistantEventID(request.CodeSessionID, messageID, index, "agent.thinking")
	if _, exists := preview.thinking[blockID]; exists {
		return
	}
	if preview.thinking == nil {
		preview.thinking = make(map[string]struct{})
	}
	eventID := request.harnessThinkingID(blockID)
	preview.thinking[blockID] = struct{}{}
	preview.eventIDs = append(preview.eventIDs, eventID)
	start := modelPreviewStart{Type: "event_start", Event: modelPreviewTarget{Type: "agent.thinking", ID: eventID}, ThreadID: request.toolThreadID}
	s.publishHarnessPreview(ctx, request, 0, start)
}

func (s *Service) publishHarnessPreview(ctx context.Context, request *ModelRequest, sequence uint64, payload any) bool {
	raw, err := json.Marshal(payload)
	if err == nil {
		err = s.sink.PublishCodeSessionStreamEvent(ctx, CodeSessionStreamRoute{
			CodeSessionID: request.CodeSessionID, WorkspaceUUID: request.codeSession.WorkspaceUUID,
			SessionExternalID: request.codeSession.SessionExternalID, ModelRequestID: request.StartID, PreviewSequence: sequence,
		}, request.harnessLease.Epoch, raw)
	}
	if err != nil {
		s.logger.WarnContext(ctx, "publish harness model preview", "model_request_start_id", request.StartID, "error", err)
	}
	return err == nil
}

func harnessTextResult(request *ModelRequest, result ModelRequestResult) ModelRequestResult {
	preview := &request.harnessPreview
	preview.finished = true
	content, thinking := harnessModelContent(request, result.Messages)
	hasTools := len(result.ToolUseIDs) > 0 || len(result.ToolUses) > 0
	if result.ErrorType == "" && ((hasTools && !validHarnessTool(result)) || (!hasTools && len(content) == 0)) {
		result.ErrorType = "invalid_response"
	}
	result.Messages = nil
	if result.ErrorType != "" {
		result.ToolUses, result.ToolUseIDs = nil, nil
		result.EventIDs = slices.Clone(preview.eventIDs)
		return result
	}
	result.Messages = thinking
	if len(content) > 0 {
		result.Messages = append(result.Messages, ModelRequestMessage{ID: request.harnessMessageID(), Type: "agent.message", Content: content})
	}
	result.EventIDs = make([]string, 0, len(result.Messages))
	for _, event := range result.Messages {
		result.EventIDs = append(result.EventIDs, event.ID)
	}
	return result
}

func harnessModelContent(request *ModelRequest, messages []ModelRequestMessage) ([]ModelRequestContent, []ModelRequestMessage) {
	positions := request.harnessPreview.textBlocks
	content := make([]ModelRequestContent, len(positions))
	var remaining []ModelRequestContent
	var thinking []ModelRequestMessage
	for _, message := range messages {
		switch message.Type {
		case "agent.thinking":
			thinking = append(thinking, ModelRequestMessage{ID: request.harnessThinkingID(message.ID), Type: message.Type})
		case "agent.message":
			index, previewed := positions[message.ID]
			for _, block := range message.Content {
				if block.Type != "text" || block.Text == nil || *block.Text == "" {
					continue
				}
				if previewed && content[index].Text == nil {
					content[index] = block
				} else {
					remaining = append(remaining, block)
				}
			}
		}
	}
	content = slices.DeleteFunc(content, func(block ModelRequestContent) bool { return block.Text == nil })
	return slices.Concat(content, remaining), thinking
}

func validHarnessTool(result ModelRequestResult) bool {
	if len(result.ToolUses) != 1 || len(result.ToolUseIDs) != 1 {
		return false
	}
	tool := result.ToolUses[0]
	if tool.ID == "" || tool.ID != result.ToolUseIDs[0] || tool.InputRejected ||
		len(tool.Input) > 32<<10 || len(tool.Input) == 0 || tool.Input[0] != '{' || !json.Valid(tool.Input) {
		return false
	}
	switch tool.Name {
	case sessioncontract.HarnessAskUserQuestion:
		_, err := sessioncontract.ParseHarnessChoiceQuestion(tool.Input)
		return err == nil
	case sessioncontract.HarnessSubscribe:
		return true
	case sessioncontract.HarnessCancelSubscription:
		_, err := sessioncontract.ParseHarnessSubscriptionID(tool.Input)
		return err == nil
	default:
		return isHarnessDelegationTool(tool.Name) || isSandboxToolName(managedAgentToolName(tool.Name))
	}
}

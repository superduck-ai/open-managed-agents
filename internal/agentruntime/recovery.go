package agentruntime

import (
	"context"
	"encoding/json"

	"charm.land/fantasy"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

func unresolvedCalls(entries []codesessions.HostHistoryEntry, store *historyStore) ([]fantasy.ToolCallPart, error) {
	pending := make(map[string]fantasy.ToolCallPart)
	dispatched := make(map[string]bool)
	var order []string
	store.index = 0
	for _, entry := range entries {
		if entry.RunID != store.runID {
			continue
		}
		if entry.Role == "tool_dispatch" {
			dispatched[entry.ID] = true
		}
		if entry.Role != "message" {
			continue
		}
		store.index++
		var message fantasy.Message
		if err := json.Unmarshal(entry.Payload, &message); err != nil {
			return nil, err
		}
		for _, part := range message.Content {
			switch content := part.(type) {
			case fantasy.ToolCallPart:
				if !content.ProviderExecuted {
					pending[content.ToolCallID] = content
					order = append(order, content.ToolCallID)
				}
			case fantasy.ToolResultPart:
				delete(pending, content.ToolCallID)
			}
		}
	}
	var calls []fantasy.ToolCallPart
	for _, id := range order {
		if call, found := pending[id]; found && !dispatched[store.id("tool-dispatch:"+id)] {
			calls = append(calls, call)
			delete(pending, id)
		}
	}
	return calls, nil
}

func resolveUndispatched(ctx context.Context, store *historyStore, current *turn) error {
	entries, err := store.worker.LoadHistory(ctx)
	if err != nil {
		return err
	}
	calls, err := unresolvedCalls(entries, store)
	if err != nil || len(calls) == 0 {
		return err
	}
	var results []fantasy.Message
	for _, call := range calls {
		results = append(results, fantasy.Message{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{fantasy.ToolResultPart{ToolCallID: call.ToolCallID, Output: fantasy.ToolResultOutputContentError{Error: ErrToolNotExecuted}}}})
	}
	if err := store.Append(ctx, store.codeSessionID, results); err != nil {
		return err
	}
	if current != nil {
		for _, call := range calls {
			if association, found := current.tools[call.ToolCallID]; found {
				if err := current.publish(ctx, publicEvent{Type: association.resultType, ID: store.id("tool-result:" + call.ToolCallID), ToolUseID: association.publicID, IsError: true, Content: []publicContent{{Type: "text", Text: ErrToolNotExecuted.Error()}}}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

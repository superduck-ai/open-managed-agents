package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/yourbatis"
)

func harnessRootKey(ref HarnessTurnRef) harnessThreadKey {
	return harnessThreadKey{Scope: ref.conversationScope(), UUID: ref.SessionUUID}
}

func (r harnessTurnRow) historyKey() harnessThreadKey {
	key := harnessRootKey(r.reference())
	if r.SubagentThreadUUID != nil {
		key.UUID = *r.SubagentThreadUUID
	}
	return key
}

func (r harnessTurnRow) historyInputID() string {
	if r.DelegationToolUUID != nil {
		return *r.DelegationToolUUID
	}
	return r.InputEventUUID
}

func (r harnessTurnRow) historyTurnUUID() string {
	if r.DelegationToolUUID != nil {
		return *r.DelegationToolUUID
	}
	return r.UUID
}

func (t HarnessTool) conversationCall() harness.ToolCall {
	return harness.ToolCall{ID: harness.ToolCallID(t.CallID), Name: t.Name, Arguments: string(t.Arguments)}
}

func recordHarnessModelHistory(ctx context.Context, executor yourbatis.Executor, sessionID string, turn harnessTurnRow, completion HarnessModelCompletion) (HarnessTool, error) {
	var call harness.ToolCall
	switch pending := completion.Pending.(type) {
	case nil:
	case harness.ToolWork:
		call = pending.Call
	case harness.UserChoiceWork:
		call = pending.Call
	default:
		return HarnessTool{}, ErrInvalidState
	}
	if call != completion.Message.Call || (completion.Pending != nil && (call.ID == "" || call.Name == "")) {
		return HarnessTool{}, ErrInvalidState
	}
	historyTurnUUID := turn.historyTurnUUID()
	key := turn.historyKey()
	state, err := lockHarnessThreadState(ctx, executor, key)
	if err != nil {
		return HarnessTool{}, err
	}
	input, err := harnessCompletionInput(ctx, executor, sessionID, turn, completion.UserText)
	if err != nil {
		return HarnessTool{}, err
	}
	messageID := fmt.Sprintf("transition:%s:%d:0", key.UUID, state.Version+1)
	messages := []harness.MessageWithID{input, {ID: messageID, Message: completion.Message}}
	if err := appendHarnessTransition(ctx, executor, key, &historyTurnUUID, state, messages); err != nil {
		return HarnessTool{}, err
	}
	if completion.Pending == nil {
		return HarnessTool{}, nil
	}
	tool := HarnessTool{UUID: uuid.NewString(), CallID: string(call.ID), Name: call.Name,
		Arguments: json.RawMessage(call.Arguments)}
	err = requireHarnessLease(NewCloudHarnessMapper(executor).AttachTool(ctx, harnessToolRecord{
		Key: key, MessageID: messageID, TurnUUID: turn.UUID, ToolUUID: tool.UUID,
	}))
	return tool, err
}

func harnessCompletionInput(ctx context.Context, executor yourbatis.Executor, sessionID string, turn harnessTurnRow, userText string) (harness.MessageWithID, error) {
	switch turn.TaskKind {
	case "":
		if userText != "" {
			return harness.MessageWithID{ID: turn.InputEventUUID, Message: harness.UserMessage{Text: userText}}, nil
		}
	case HarnessSubagentTask:
		if turn.DelegationToolUUID != nil {
			prompt, err := loadHarnessDelegationPrompt(ctx, executor, turn.reference(), sessionID, *turn.DelegationToolUUID)
			return harness.MessageWithID{ID: *turn.DelegationToolUUID, Message: harness.UserMessage{Text: prompt}}, err
		}
	case HarnessModelTask:
		tool, err := harnessHistoryTool(ctx, NewCloudHarnessMapper(executor), turn)
		if err != nil {
			return harness.MessageWithID{}, err
		}
		if tool.Result != nil {
			return harnessHistoryToolResult(tool), nil
		}
	}
	return harness.MessageWithID{}, ErrInvalidState
}

func harnessHistoryTool(ctx context.Context, mapper CloudHarnessMapper, turn harnessTurnRow) (harnessToolRow, error) {
	if turn.ToolUUID == nil {
		return harnessToolRow{}, ErrInvalidState
	}
	tool, found, err := mapper.FindTool(ctx, turn.reference(), *turn.ToolUUID)
	if err == nil && !found {
		err = ErrInvalidState
	}
	return tool, err
}

func harnessHistoryToolResult(tool harnessToolRow) harness.MessageWithID {
	return harness.MessageWithID{ID: tool.UUID + ":result",
		Message: harness.ToolResult{CallID: harness.ToolCallID(tool.CallID), Text: *tool.Result, Failed: tool.Failed}}
}

func settleFailedHarnessHistory(ctx context.Context, executor yourbatis.Executor, turn harnessTurnRow) error {
	key := turn.historyKey()
	state, err := lockHarnessThreadState(ctx, executor, key)
	if err != nil {
		return err
	}
	if turn.TaskKind == HarnessModelTask {
		tool, err := harnessHistoryTool(ctx, NewCloudHarnessMapper(executor), turn)
		if err != nil {
			return err
		}
		if tool.Result == nil {
			return ErrInvalidState
		}
		return appendHarnessTransition(ctx, executor, key, new(turn.historyTurnUUID()), state, []harness.MessageWithID{harnessHistoryToolResult(tool)})
	}
	if err := discardUnvalidatedHarnessHistory(ctx, NewHarnessConversationMapper(executor), key, turn.historyTurnUUID(), &state); err != nil {
		return err
	}
	state.Version++
	return saveHarnessTransition(ctx, executor, key, state)
}

func discardUnvalidatedHarnessHistory(ctx context.Context, mapper HarnessConversationMapper, key harnessThreadKey, turnUUID string, state *harnessThreadState) error {
	records, err := mapper.ListTurnMessages(ctx, key, turnUUID)
	if err != nil {
		return err
	}
	for _, record := range records {
		message, err := decodeHarnessMessage(record)
		if err != nil {
			return err
		}
		if _, committed := message.(harness.AssistantMessage); committed {
			return nil
		}
	}
	if len(records) == 0 {
		return nil
	}
	removed, err := mapper.DiscardTurn(ctx, key, turnUUID)
	if err != nil {
		return err
	}
	state.MessageCount -= removed
	return nil
}

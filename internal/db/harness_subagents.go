package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/yourbatis"
)

type HarnessDelegation struct {
	ThreadID       string
	AgentSnapshot  json.RawMessage
	Prompt         string
	InheritContext bool
}

func (d *DB) HarnessSubagent(ctx context.Context, lease HarnessTurnLease) (*SessionThread, error) {
	row, found, err := NewCloudHarnessMapper(d.mapperDB).Find(ctx, lease.Ref)
	if err != nil {
		return nil, err
	}
	if !found || row.State != "running" || row.WorkerEpoch != lease.Epoch {
		return nil, ErrWorkerEpochMismatch
	}
	return harnessSubagentThread(ctx, d.mapperDB, row)
}

func harnessSubagentThread(ctx context.Context, executor yourbatis.Executor, row harnessTurnRow) (*SessionThread, error) {
	if row.SubagentThreadUUID == nil {
		return nil, nil
	}
	thread, err := NewSessionThreadMapper(executor).FindByUUID(ctx, row.OrganizationUUID, row.WorkspaceUUID, row.SessionUUID, *row.SubagentThreadUUID)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return new(thread.thread()), nil
}

func (d *DB) StartHarnessSubagent(ctx context.Context, lease HarnessTurnLease, code CodeSession, tool HarnessTool, delegation HarnessDelegation) ([]SessionEvent, error) {
	var events []SessionEvent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		session, err := lockSessionForEvents(ctx, NewSessionMapper(executor), lease.Ref.WorkspaceUUID, code.SessionExternalID)
		if err != nil {
			return err
		}
		if !harnessSessionMatches(session, lease.Ref) || code.UUID != lease.CodeSessionUUID {
			return ErrWorkerEpochMismatch
		}
		if _, _, err := NewCodeSessionMapper(executor).LockLatestInputState(ctx, session.WorkspaceUUID, session.UUID); err != nil {
			return err
		}
		mapper := NewCloudHarnessMapper(executor)
		row, found, err := mapper.Lock(ctx, lease.Ref)
		if err != nil {
			return err
		}
		if !found || row.State != "running" || row.WorkerEpoch != lease.Epoch || row.TaskKind != HarnessToolTask ||
			row.task().UUID != tool.UUID || row.SubagentThreadUUID != nil {
			return ErrWorkerEpochMismatch
		}
		events, err = startHarnessSubagentTx(ctx, executor, session, lease, tool, delegation)
		return err
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

func startHarnessSubagentTx(ctx context.Context, executor yourbatis.Executor, session Session, lease HarnessTurnLease, tool HarnessTool, delegation HarnessDelegation) ([]SessionEvent, error) {
	mapper := NewSessionThreadMapper(executor)
	primaryRow, err := mapper.FindPrimary(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, err
	}
	primary := primaryRow.thread()
	now := time.Now().UTC()
	child, err := resolveHarnessDelegationThread(ctx, executor, session, primary, lease, tool, delegation, now)
	if err != nil {
		return nil, err
	}
	if err := advanceHarnessTask(ctx, NewCloudHarnessMapper(executor), harnessTaskParams{
		Lease: lease, TaskUUID: uuid.NewString(), ToolUUID: tool.UUID, Kind: HarnessSubagentTask, State: "pending",
		SubagentThreadUUID: &child.UUID, DelegationToolUUID: &tool.UUID,
	}); err != nil {
		return nil, err
	}
	events, err := harnessDelegationEvents(primary, child, tool.UUID, delegation.Prompt, delegation.ThreadID == "", now)
	if err != nil {
		return nil, err
	}
	batch, err := insertSessionHistoryTx(ctx, executor, session, events)
	return batch.Events, err
}

func (d *DB) HarnessDelegationPrompt(ctx context.Context, lease HarnessTurnLease, sessionID string) (string, error) {
	return loadHarnessDelegationPrompt(ctx, d.mapperDB, lease.Ref, sessionID, lease.DelegationToolUUID)
}

func loadHarnessDelegationPrompt(ctx context.Context, executor yourbatis.Executor, ref HarnessTurnRef, sessionID, delegationID string) (string, error) {
	id := derivedStatusEventID(delegationID, "delegate")
	row, err := NewSessionEventMapper(executor).FindByExternalID(ctx, ref.WorkspaceUUID, sessionID, id)
	if err != nil {
		return "", mapNoRows(err)
	}
	if row.OrganizationUUID != ref.OrganizationUUID || row.SessionUUID != ref.SessionUUID || row.EventType != "agent.thread_message_sent" {
		return "", ErrInvalidState
	}
	var message harnessThreadMessage
	if err := json.Unmarshal(row.Payload, &message); err != nil {
		return "", err
	}
	if len(message.Content) != 1 || message.Content[0].Type != "text" || message.Content[0].Text == "" {
		return "", ErrInvalidState
	}
	return message.Content[0].Text, nil
}

func resolveHarnessDelegationThread(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, lease HarnessTurnLease, tool HarnessTool, delegation HarnessDelegation, at time.Time) (SessionThread, error) {
	mapper := NewSessionThreadMapper(executor)
	conversation := NewHarnessConversationMapper(executor)
	if delegation.ThreadID != "" {
		row, err := mapper.FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, delegation.ThreadID)
		if err != nil {
			return SessionThread{}, mapNoRows(err)
		}
		child := row.thread()
		if child.ParentThreadUUID == nil || *child.ParentThreadUUID != primary.UUID || child.Status != "idle" || child.ArchivedAt != nil {
			return SessionThread{}, ErrSessionInputConflict
		}
		state, found, err := conversation.FindThread(ctx, harnessThreadKey{Scope: lease.Ref.conversationScope(), UUID: child.UUID})
		if err != nil {
			return SessionThread{}, err
		}
		if !found || state.Closed {
			return SessionThread{}, ErrSessionInputConflict
		}
		return child, nil
	}
	id, err := ids.New("sthr_")
	if err != nil {
		return SessionThread{}, err
	}
	child := SessionThread{
		UUID: uuid.NewString(), ExternalID: id, OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID,
		SessionUUID: session.UUID, SessionExternalID: session.ExternalID, ParentThreadUUID: &primary.UUID,
		ParentThreadExternalID: &primary.ExternalID, AgentSnapshot: delegation.AgentSnapshot, Status: "idle", CreatedAt: at,
		Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`),
	}
	if _, err := mapper.Insert(ctx, sessionThreadWriteParameters(child)); err != nil {
		return SessionThread{}, err
	}
	key := harnessThreadKey{Scope: lease.Ref.conversationScope(), UUID: child.UUID}
	spawn, err := harnessDelegationSpawnContext(ctx, executor, lease.Ref, tool, delegation.InheritContext)
	if err != nil {
		return SessionThread{}, err
	}
	if err := insertSpawnedHarnessThread(ctx, conversation, key, session.UUID, harness.ToolCallID(tool.CallID), spawn); err != nil {
		return SessionThread{}, err
	}
	return child, nil
}

func harnessDelegationSpawnContext(ctx context.Context, executor yourbatis.Executor, ref HarnessTurnRef, tool HarnessTool, inherit bool) (harness.SpawnContext, error) {
	var spawn harness.SpawnContext
	if !inherit {
		return spawn, nil
	}
	state, err := lockHarnessThreadState(ctx, executor, harnessRootKey(ref))
	if err != nil {
		return spawn, err
	}
	if state.MessageCount == 0 || tool.Result != nil {
		return spawn, ErrInvalidState
	}
	spawn.InheritUpTo = new(state.MessageCount - 1)
	return spawn, nil
}

func completeHarnessSubagentTx(ctx context.Context, executor yourbatis.Executor, session Session, code CodeSession, lease HarnessTurnLease, row harnessTurnRow, completion *HarnessModelCompletion) ([]SessionEvent, error) {
	child, err := harnessSubagentThread(ctx, executor, row)
	if err != nil || child == nil || row.DelegationToolUUID == nil {
		return nil, errors.Join(err, ErrInvalidState)
	}
	primaryRow, err := NewSessionThreadMapper(executor).FindPrimary(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, err
	}
	mapper := NewCloudHarnessMapper(executor)
	tool, found, err := mapper.FindTool(ctx, lease.Ref, *row.DelegationToolUUID)
	if err != nil {
		return nil, err
	}
	if !found || tool.DelegationToolUUID != nil {
		return nil, ErrInvalidState
	}
	report, reason := "The subagent exhausted its model retries.", "retries_exhausted"
	if completion == nil {
		if err := settleFailedHarnessHistory(ctx, executor, row); err != nil {
			return nil, err
		}
	} else {
		report, reason = completion.Message.Text, "end_turn"
	}
	result := harnessSubagentResultText(child.ExternalID, report)
	if err := requireHarnessLease(mapper.FinishTool(ctx, harnessToolResultParams{
		Ref: lease.Ref, ToolUUID: tool.UUID, Text: result, Failed: completion == nil,
	})); err != nil {
		return nil, err
	}
	if err := advanceHarnessTask(ctx, mapper, harnessTaskParams{
		Lease: lease, TaskUUID: uuid.NewString(), ToolUUID: tool.UUID, Kind: HarnessModelTask, State: "pending",
	}); err != nil {
		return nil, err
	}
	events, err := harnessSubagentReportEvents(code, primaryRow.thread(), *child, tool, report, result, reason, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	batch, err := insertSessionHistoryTx(ctx, executor, session, events)
	return batch.Events, err
}

func harnessSubagentResultText(threadID, report string) string {
	text := "Subagent " + threadID + ":\n" + strings.ReplaceAll(report, "\x00", "")
	if len(text) <= sessioncontract.HarnessToolResultMaxBytes {
		return text
	}
	const suffix = "\n[Subagent report truncated]"
	end := sessioncontract.HarnessToolResultMaxBytes - len(suffix)
	for !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + suffix
}

type harnessThreadMessage struct {
	ID         string              `json:"id"`
	Type       string              `json:"type"`
	ThreadID   string              `json:"session_thread_id"`
	To         string              `json:"to_session_thread_id,omitempty"`
	From       string              `json:"from_session_thread_id,omitempty"`
	OriginalID string              `json:"original_event_id,omitempty"`
	Content    []harnessThreadText `json:"content"`
}

type harnessThreadText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func harnessThreadMessages(primary, child SessionThread, id, text string, fromChild bool, at time.Time) ([]SessionEvent, error) {
	content := []harnessThreadText{{Type: "text", Text: strings.ReplaceAll(text, "\x00", "")}}
	main := harnessThreadMessage{ID: id, ThreadID: primary.ExternalID, Content: content}
	mirror := harnessThreadMessage{ID: derivedStatusEventID(id, "child"), ThreadID: child.ExternalID, OriginalID: id, Content: content}
	if fromChild {
		main.Type, main.From = "agent.thread_message_received", child.ExternalID
		mirror.Type, mirror.To = "agent.thread_message_sent", primary.ExternalID
	} else {
		main.Type, main.To = "agent.thread_message_sent", child.ExternalID
		mirror.Type, mirror.From = "agent.thread_message_received", primary.ExternalID
	}
	parentEvent, err := newHarnessSubagentEvent(main.ID, main.Type, &primary.ExternalID, main, at)
	if err != nil {
		return nil, err
	}
	childEvent, err := newHarnessSubagentEvent(mirror.ID, mirror.Type, &child.ExternalID, mirror, at)
	return []SessionEvent{parentEvent, childEvent}, err
}

func harnessDelegationEvents(primary, child SessionThread, id, prompt string, created bool, at time.Time) ([]SessionEvent, error) {
	var events []SessionEvent
	if created {
		eventID := derivedStatusEventID(id, "created")
		payload := struct {
			ID       string          `json:"id"`
			Type     string          `json:"type"`
			ThreadID string          `json:"session_thread_id"`
			ParentID string          `json:"parent_session_thread_id"`
			Agent    json.RawMessage `json:"agent"`
		}{eventID, "session.thread_created", child.ExternalID, primary.ExternalID, child.AgentSnapshot}
		event, err := newHarnessSubagentEvent(eventID, payload.Type, &primary.ExternalID, payload, at)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	running, err := harnessSubagentStatus(child, id, "session.thread_status_running", nil, at)
	if err != nil {
		return nil, err
	}
	messages, err := harnessThreadMessages(primary, child, derivedStatusEventID(id, "delegate"), prompt, false, at)
	return append(append(events, running), messages...), err
}

func harnessSubagentReportEvents(code CodeSession, primary, child SessionThread, tool harnessToolRow, report, result, reason string, at time.Time) ([]SessionEvent, error) {
	messages, err := harnessThreadMessages(primary, child, derivedStatusEventID(tool.UUID, "report"), report, true, at)
	if err != nil {
		return nil, err
	}
	idle, err := harnessSubagentStatus(child, tool.UUID, "session.thread_status_idle", &sessionStopReason{Type: reason}, at)
	if err != nil {
		return nil, err
	}
	id := derivedStatusEventID(tool.UUID, "tool_result")
	payload := struct {
		ID        string              `json:"id"`
		Type      string              `json:"type"`
		ToolUseID string              `json:"tool_use_id"`
		Content   []harnessThreadText `json:"content"`
		IsError   bool                `json:"is_error"`
	}{id, "agent.tool_result", maevents.ToolUseEventID(code.ExternalID, tool.CallID, ""), []harnessThreadText{{Type: "text", Text: result}}, reason != "end_turn"}
	event, err := newHarnessSubagentEvent(id, payload.Type, &primary.ExternalID, payload, at)
	return append(messages, idle, event), err
}

func harnessSubagentStatus(child SessionThread, id, kind string, reason *sessionStopReason, at time.Time) (SessionEvent, error) {
	eventID := derivedStatusEventID(id, kind)
	event, err := newHarnessSubagentEvent(eventID, kind, nil, sessionStatusPayload{ID: eventID, Type: kind, SessionThreadID: child.ExternalID, StopReason: reason}, at)
	event.StatusThreadID = child.ExternalID
	return event, err
}

func newHarnessSubagentEvent(id, kind string, threadID *string, payload any, at time.Time) (SessionEvent, error) {
	data, err := json.Marshal(payload)
	return SessionEvent{UUID: uuid.NewString(), ExternalID: id, EventType: kind, ThreadExternalID: threadID, Payload: data, CreatedAt: at, ProcessedAt: at}, err
}

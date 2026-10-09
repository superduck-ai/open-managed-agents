package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

type historyStore struct {
	worker        nativeWorker
	codeSessionID string
	runID         string
	inputEventID  string
	index         int
	accepted      bool
}

func (h *historyStore) Load(ctx context.Context, _ string) ([]fantasy.Message, error) {
	entries, err := h.worker.LoadHistory(ctx)
	if err != nil {
		return nil, err
	}
	var messages []fantasy.Message
	for _, entry := range entries {
		if entry.InputEventID == h.inputEventID && entry.Role == "run_started" {
			return nil, ErrInputAlreadyAccepted
		}
		if entry.Role != "message" {
			continue
		}
		var message fantasy.Message
		if err := json.Unmarshal(entry.Payload, &message); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (h *historyStore) Append(ctx context.Context, _ string, messages []fantasy.Message) error {
	entries := make([]codesessions.HostHistoryEntry, 0, len(messages)+1)
	if !h.accepted {
		if len(messages) == 0 || messages[0].Role != fantasy.MessageRoleUser {
			return ErrInvalidInput
		}
		entries = append(entries, h.marker("run_started", "start"))
	}
	for index, message := range messages {
		payload, err := json.Marshal(message)
		if err != nil {
			return err
		}
		entries = append(entries, codesessions.HostHistoryEntry{ID: h.id(fmt.Sprintf("message:%d", h.index+index)), Role: "message", RunID: h.runID, InputEventID: h.inputEventID, Payload: payload})
	}
	if err := h.worker.AppendHistory(ctx, entries); err != nil {
		return err
	}
	h.index += len(messages)
	if !h.accepted {
		h.accepted = true
		return h.worker.Acknowledge(ctx, h.inputEventID, "processed")
	}
	return nil
}

func (h *historyStore) marker(role, key string) codesessions.HostHistoryEntry {
	return codesessions.HostHistoryEntry{ID: h.id(key), Role: role, RunID: h.runID, InputEventID: h.inputEventID, Payload: json.RawMessage(`{}`)}
}

func (h *historyStore) id(key string) string {
	return codesessions.HostRunID(h.codeSessionID, h.runID+":"+key)
}

func interruptedRuns(entries []codesessions.HostHistoryEntry) []codesessions.HostHistoryEntry {
	started := make(map[string]codesessions.HostHistoryEntry)
	var order []string
	for _, entry := range entries {
		switch entry.Role {
		case "run_started":
			if _, exists := started[entry.RunID]; !exists {
				order = append(order, entry.RunID)
			}
			started[entry.RunID] = entry
		case "run_finished":
			delete(started, entry.RunID)
		}
	}
	var pending []codesessions.HostHistoryEntry
	for _, runID := range order {
		if entry, ok := started[runID]; ok {
			pending = append(pending, entry)
		}
	}
	return pending
}

package codesessions

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type HostHistoryEntry struct {
	ID           string          `json:"id"`
	Role         string          `json:"role"`
	RunID        string          `json:"run_id,omitempty"`
	InputEventID string          `json:"input_event_id,omitempty"`
	Payload      json.RawMessage `json:"payload"`
	Compaction   bool            `json:"compaction,omitempty"`
}

type hostHistoryEnvelope struct {
	Type          string           `json:"type"`
	UUID          string           `json:"uuid"`
	SchemaVersion int              `json:"host_schema_version"`
	Entry         HostHistoryEntry `json:"host_entry"`
}

func hostHistoryInput(codeSessionID string, entry HostHistoryEntry) (db.AppendCodeSessionInternalEventInput, error) {
	if entry.ID == "" || entry.RunID == "" {
		return db.AppendCodeSessionInternalEventInput{}, ErrHostHistoryInvalid
	}
	switch entry.Role {
	case "message", "run_started", "run_finished", "tool_dispatch", "tool_result", "compaction":
	default:
		return db.AppendCodeSessionInternalEventInput{}, ErrHostHistoryInvalid
	}
	if !json.Valid(entry.Payload) {
		return db.AppendCodeSessionInternalEventInput{}, ErrHostHistoryInvalid
	}
	payload, err := json.Marshal(hostHistoryEnvelope{Type: "system", UUID: entry.ID, SchemaVersion: 1, Entry: entry})
	if err != nil {
		return db.AppendCodeSessionInternalEventInput{}, err
	}
	metadata, err := BuildEventMetadata(codeSessionID, "internal", payload)
	if err != nil {
		return db.AppendCodeSessionInternalEventInput{}, err
	}
	return db.AppendCodeSessionInternalEventInput{
		ExternalID: stablePublicEventID(codeSessionID, "host-history:"+entry.ID), EventType: "system", PayloadUUID: entry.ID,
		IsCompaction: entry.Compaction, Payload: payload, PayloadHash: metadata.PayloadHash, IdempotencyKey: metadata.IdempotencyKey,
	}, nil
}

func (w *HostWorker) AppendHistory(ctx context.Context, entries []HostHistoryEntry) error {
	inputs := make([]db.AppendCodeSessionInternalEventInput, 0, len(entries))
	for _, entry := range entries {
		input, err := hostHistoryInput(w.record.ExternalID, entry)
		if err != nil {
			return err
		}
		inputs = append(inputs, input)
	}
	_, err := w.service.eventPayloads.AppendInternal(ctx, w.record, w.epoch, inputs)
	return err
}

func (w *HostWorker) LoadHistory(ctx context.Context) ([]HostHistoryEntry, error) {
	if err := w.service.db.ValidateCodeSessionWorkerEpoch(ctx, w.record.ExternalID, w.epoch); err != nil {
		return nil, err
	}
	var history []HostHistoryEntry
	var cursor int64
	for {
		events, more, err := w.service.eventPayloads.ListCodeSessionInternalEventsPage(ctx, db.ListCodeSessionInternalEventsPageParams{
			WorkspaceUUID: w.record.WorkspaceUUID, CodeSessionExternalID: w.record.ExternalID, AfterSequence: cursor, Limit: internalEventsPageSize,
		})
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			var envelope hostHistoryEnvelope
			if err := json.Unmarshal(event.Payload, &envelope); err != nil {
				return nil, err
			}
			if envelope.SchemaVersion != 1 || envelope.UUID != envelope.Entry.ID {
				return nil, ErrHostHistoryInvalid
			}
			if _, err := hostHistoryInput(w.record.ExternalID, envelope.Entry); err != nil {
				return nil, err
			}
			history = append(history, envelope.Entry)
			cursor = event.SequenceNum
		}
		if !more {
			return history, nil
		}
		if len(events) == 0 {
			return nil, ErrHostHistoryInvalid
		}
	}
}

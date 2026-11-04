package codesessions

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func isDSHCodeSession(code db.CodeSession) bool {
	var metadata codeSessionTransportMetadata
	return json.Unmarshal(code.Metadata, &metadata) == nil && metadata.Config.WorkerProtocol == "dsr1"
}

func (s *Service) queueDSHPublicEvents(ctx context.Context, code db.CodeSession, events []db.SessionEvent) error {
	payloads := make([]json.RawMessage, 0)
	scope := db.DSHScope{OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, CodeSessionUUID: code.UUID, CodeSessionID: code.ExternalID, SessionID: code.SessionExternalID}
	for _, event := range events {
		var fields map[string]json.RawMessage
		switch event.EventType {
		case "user.message":
			raw, err := s.dshMessagePayload(ctx, code, event)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &fields); err != nil {
				return err
			}
		case "user.interrupt":
			fields = make(map[string]json.RawMessage)
			var target struct {
				ThreadID string `json:"session_thread_id"`
			}
			if err := json.Unmarshal(event.Payload, &target); err != nil {
				return err
			}
			if target.ThreadID != "" {
				primary, _, err := s.db.GetPrimarySessionThreadExternalID(ctx, code.WorkspaceUUID, code.SessionExternalID)
				if err != nil {
					return err
				}
				if target.ThreadID == primary {
					target.ThreadID = code.ExternalID
				}
				setRawJSONField(fields, "session_thread_id", target.ThreadID)
			}
		case "user.tool_confirmation", "user.custom_tool_result", "user.tool_result":
			permission, found, err := s.db.ResolvedDSHPermissionForInput(ctx, scope, event)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			fields = make(map[string]json.RawMessage)
			setRawJSONField(fields, "request_id", permission.RequestID)
			fields["resolution"] = permission.Decision
		default:
			continue
		}
		kind := event.EventType
		lane := "control"
		if kind == "user.message" {
			lane = "task"
		}
		if kind == "user.tool_confirmation" || kind == "user.custom_tool_result" || kind == "user.tool_result" {
			kind = "permission.resolved"
		}
		setRawJSONField(fields, "id", event.ExternalID)
		setRawJSONField(fields, "uuid", event.ExternalID)
		setRawJSONField(fields, "type", kind)
		setRawJSONField(fields, "kind", kind)
		setRawJSONField(fields, "lane", lane)
		raw, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		payloads = append(payloads, raw)
	}
	if len(payloads) == 0 {
		return nil
	}
	if err := s.QueueRawPublicSessionEvents(ctx, code, payloads); err != nil {
		return err
	}
	s.notifyBrainWork(ctx, code.ExternalID)
	return s.resumeSandboxForCodeSession(ctx, code)
}

func (s *Service) dshMessagePayload(ctx context.Context, code db.CodeSession, event db.SessionEvent) (json.RawMessage, error) {
	materialized, err := s.materializeMessageAttachments(ctx, event, true)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(materialized.Payload, &fields); err != nil {
		return nil, err
	}
	setRawJSONField(fields, "oma_refresh_tools", true)
	setRawJSONField(fields, "id", event.ExternalID)
	setRawJSONField(fields, "uuid", event.ExternalID)
	setRawJSONField(fields, "type", "user.message")
	setRawJSONField(fields, "kind", "user.message")
	setRawJSONField(fields, "lane", "task")
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	contexts, err := loadSystemContext(ctx, db.ListSessionEventsPageParams{WorkspaceUUID: code.WorkspaceUUID, SessionExternalID: code.SessionExternalID, PrimaryOnly: true, Limit: 500, Order: "asc", Types: []string{"system.message"}, CreatedAtLTE: &event.CreatedAt}, s.eventPayloads.ListSessionEventsPage)
	if err != nil {
		return nil, err
	}
	return workerPayloadWithSystemContext(raw, contexts)
}

func (s *Service) prepareDSHActivation(ctx context.Context, snapshot activationSnapshot, batch *inboundPublicationBatch) error {
	for _, event := range snapshot.events {
		if event.EventType != "user.message" || !event.ProcessedAt.IsZero() {
			continue
		}
		restored, err := s.eventPayloads.RestorePublic(ctx, event)
		if err != nil {
			return err
		}
		raw, err := s.dshMessagePayload(ctx, snapshot.codeSession, restored)
		if err != nil {
			return err
		}
		prepared, err := s.prepareInboundEvent(ctx, snapshot.codeSession, raw, "dsr-activation", event.ExternalID)
		if err != nil {
			return err
		}
		batch.events = append(batch.events, prepared)
	}
	return nil
}

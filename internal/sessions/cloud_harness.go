package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func (h *Handler) executionRuntime(environment db.Environment) string {
	if !h.cfg.EnvironmentRunner.CloudHarness {
		return ""
	}
	var cfg struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(environment.Config, &cfg) == nil && cfg.Type == "cloud" {
		return sessioncontract.CloudHarnessRuntime
	}
	return ""
}

func validateHarnessInputs(session db.Session, events []db.SessionEvent) error {
	if session.ExecutionRuntime != sessioncontract.CloudHarnessRuntime {
		return nil
	}
	for _, event := range events {
		if len(event.Payload) > sessioncontract.HarnessInputMaxBytes {
			return errors.New("cloud harness accepts input events up to 16 KiB")
		}
		var err error
		switch event.EventType {
		case "user.message":
			_, err = sessioncontract.ParseHarnessUserInput(event.Payload)
		case "user.interrupt":
			continue
		case "user.custom_tool_result":
			_, err = sessioncontract.ParseHarnessChoiceResponse(event.Payload)
		default:
			return errors.New("cloud harness accepts text messages, interrupts, and answers to pending questions")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) PublishHarnessModelEvents(ctx context.Context, codeSession db.CodeSession, lease db.HarnessTurnLease, phase db.HarnessModelPhase, payloads []json.RawMessage, completion *harness.AssistantMessage) error {
	session, found, err := h.db.GetSessionByUUID(ctx, lease.Ref.WorkspaceUUID, lease.Ref.SessionUUID)
	if err != nil {
		return err
	}
	if !found || session.OrganizationUUID != lease.Ref.OrganizationUUID {
		return db.ErrNotFound
	}
	var events []db.SessionEvent
	now := time.Now().UTC()
	for _, raw := range payloads {
		batch, err := h.sessionEventsFromCodeSessionPayload(ctx, session, codeSession.ExternalID, raw, now)
		if err != nil {
			return err
		}
		events = append(events, batch...)
	}
	var committed *db.HarnessModelCompletion
	if phase == db.HarnessModelEnd && completion != nil {
		committed, err = h.harnessModelCompletion(ctx, lease.Ref, *completion)
		if err != nil {
			return err
		}
	}
	events, err = h.db.RecordHarnessModelEvents(ctx, lease, codeSession, phase, events, committed)
	if err != nil {
		return err
	}
	h.publishSessionEvents(ctx, events)
	h.enqueueWebhooksForSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, events)
	return h.enforceBudgetAfterEvents(ctx, session.WorkspaceUUID, session.ExternalID, events)
}

func (h *Handler) harnessModelCompletion(ctx context.Context, ref db.HarnessTurnRef, message harness.AssistantMessage) (*db.HarnessModelCompletion, error) {
	completion := &db.HarnessModelCompletion{Message: message}
	task, _, err := h.db.HarnessTurnTask(ctx, ref)
	if err != nil || task.Kind != "" {
		return completion, err
	}
	input, found, err := h.db.FindHarnessInput(ctx, ref)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	inputMessage, err := h.eventPayloads.RestoreHarnessInput(ctx, input)
	if err != nil {
		return nil, err
	}
	completion.UserText = inputMessage.Text
	return completion, nil
}

func (h *Handler) PublishHarnessToolResult(ctx context.Context, codeSession db.CodeSession, lease db.HarnessTurnLease, taskUUID string, result sessioncontract.HarnessToolResult, payload json.RawMessage) error {
	session, found, err := h.db.GetSessionByUUID(ctx, lease.Ref.WorkspaceUUID, lease.Ref.SessionUUID)
	if err != nil {
		return err
	}
	if !found || session.OrganizationUUID != lease.Ref.OrganizationUUID {
		return db.ErrNotFound
	}
	events, err := h.sessionEventsFromCodeSessionPayload(ctx, session, codeSession.ExternalID, payload, time.Now().UTC())
	if err != nil {
		return err
	}
	events, err = h.db.FinishHarnessTool(ctx, session.ExternalID, lease, taskUUID, result, events)
	if err != nil {
		return err
	}
	h.publishSessionEvents(ctx, events)
	h.enqueueWebhooksForSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, events)
	return nil
}

func (h *Handler) PublishHarnessDelegation(ctx context.Context, code db.CodeSession, lease db.HarnessTurnLease, tool db.HarnessTool, delegation db.HarnessDelegation) error {
	events, err := h.db.StartHarnessSubagent(ctx, lease, code, tool, delegation)
	if err != nil {
		return err
	}
	h.publishSessionEvents(ctx, events)
	h.enqueueWebhooksForSessionEvents(ctx, code.WorkspaceUUID, code.SessionExternalID, events)
	return nil
}

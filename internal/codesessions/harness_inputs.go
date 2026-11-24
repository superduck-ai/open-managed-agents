package codesessions

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

type HarnessInputSenderFactory func(db.Session) cloudharness.InputSender

func (h *Handler) RunHarnessInputMaintenance(ctx context.Context) {
	if h.service.harnessInputs == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.publishReadyHarnessInputs(ctx); err != nil {
				h.logger.WarnContext(ctx, "harness wake publication failed", "error", err)
			}
		}
	}
}

func (h *Handler) publishReadyHarnessInputs(ctx context.Context) error {
	inputs, err := h.db.ListReadyHarnessInputs(ctx, 64)
	if err != nil {
		return err
	}
	var publishErr error
	for _, input := range inputs {
		if err := h.publishHarnessWake(ctx, input); err != nil {
			publishErr = errors.Join(publishErr, err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return publishErr
}

func (h *Handler) publishHarnessWake(ctx context.Context, input db.HarnessWake) error {
	var content cloudharness.Input
	switch input.Kind {
	case db.HarnessChoiceResponse:
		content = cloudharness.ChoiceResponse{}
	case db.HarnessToolResult:
		content = cloudharness.ToolResultReady{}
	default:
		return db.ErrInvalidState
	}
	ref := input.Turn
	session := db.Session{UUID: ref.SessionUUID, WorkspaceUUID: ref.WorkspaceUUID, OrganizationUUID: ref.OrganizationUUID}
	message := cloudharness.InputMessage{GroupID: cloudharness.ThreadID(session.UUID), Content: content}
	if err := h.service.harnessInputs(session).SendToInputQueue(ctx, message, input.InputUUID); err != nil {
		return err
	}
	return h.db.MarkHarnessInputPublished(ctx, input)
}

func (s *Service) WithHarnessInputSender(factory HarnessInputSenderFactory) *Service {
	s.harnessInputs = factory
	return s
}

func (s *Service) publishHarnessInputs(ctx context.Context, session db.Session, events []db.SessionEvent) error {
	for _, event := range events {
		if event.EventType != "user.custom_tool_result" && (event.EventType != "user.message" || !event.ProcessedAt.IsZero()) {
			continue
		}
		if s.harnessInputs == nil {
			return workerEventUnavailable(errors.New("harness input sender is unavailable"))
		}
		if event.EventType == "user.custom_tool_result" {
			message := cloudharness.InputMessage{GroupID: cloudharness.ThreadID(session.UUID), Content: cloudharness.ChoiceResponse{}}
			if err := s.harnessInputs(session).SendToInputQueue(ctx, message, event.UUID); err != nil {
				return workerEventUnavailable(err)
			}
			continue
		}
		restored, err := s.eventPayloads.RestorePublic(ctx, event)
		if err != nil {
			return err
		}
		input, err := sessioncontract.ParseHarnessUserInput(restored.Payload)
		if err != nil {
			return err
		}
		message := cloudharness.InputMessage{
			GroupID: cloudharness.ThreadID(session.UUID), Content: cloudharness.UserMessage{Text: input.ModelText()}, Metadata: input.Metadata,
		}
		if err := s.harnessInputs(session).SendToInputQueue(ctx, message, event.UUID); err != nil {
			return workerEventUnavailable(err)
		}
	}
	return nil
}

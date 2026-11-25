package codesessions

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/db"
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
			if err := errors.Join(h.publishReadyHarnessInputs(ctx), h.publishUnpublishedHarnessInputs(ctx)); err != nil {
				h.logger.WarnContext(ctx, "harness input publication failed", "error", err)
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
		switch event.EventType {
		case "user.custom_tool_result":
			if s.harnessInputs == nil {
				return workerEventUnavailable(errors.New("harness input sender is unavailable"))
			}
			message := cloudharness.InputMessage{GroupID: cloudharness.ThreadID(session.UUID), Content: cloudharness.ChoiceResponse{}}
			if err := s.harnessInputs(session).SendToInputQueue(ctx, message, event.UUID); err != nil {
				return workerEventUnavailable(err)
			}
		case "user.message":
			ref := db.HarnessTurnRef{UUID: event.UUID, SessionUUID: session.UUID, OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID}
			input, found, err := s.db.FindHarnessInput(ctx, ref)
			if err != nil {
				return err
			}
			if found && input.PendingPublication() {
				if err := s.publishHarnessUserInput(ctx, input, s.db.MarkHarnessUserInputPublished); err != nil {
					return workerEventUnavailable(err)
				}
			}
		}
	}
	return nil
}

func (h *Handler) publishUnpublishedHarnessInputs(ctx context.Context) error {
	inputs, err := h.db.ListUnpublishedHarnessInputs(ctx, 64)
	if err != nil {
		return err
	}
	var publishErr error
	for _, input := range inputs {
		publishErr = errors.Join(publishErr, h.service.publishHarnessUserInput(ctx, input, h.db.MarkHarnessUserInputPublished))
		if ctx.Err() != nil {
			break
		}
	}
	return publishErr
}

func (s *Service) publishHarnessUserInput(ctx context.Context, input db.HarnessInput, markPublished func(context.Context, db.HarnessTurnRef) error) error {
	if s.harnessInputs == nil {
		return errors.New("harness input sender is unavailable")
	}
	message, err := s.eventPayloads.RestoreHarnessInput(ctx, input)
	if err != nil {
		return err
	}
	ref := input.Ref
	session := db.Session{UUID: ref.SessionUUID, WorkspaceUUID: ref.WorkspaceUUID, OrganizationUUID: ref.OrganizationUUID}
	inputMessage := cloudharness.InputMessage{
		GroupID: cloudharness.ThreadID(ref.SessionUUID), Content: cloudharness.UserMessage{Text: message.Text}, Metadata: message.Metadata,
	}
	if err := s.harnessInputs(session).SendToInputQueue(ctx, inputMessage, ref.UUID); err != nil {
		return err
	}
	return markPublished(ctx, ref)
}

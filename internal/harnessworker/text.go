package harnessworker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/cloudharness/fantasyadapter"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func (w *Worker) executeTextTurn(ctx context.Context, claim db.HarnessTurnClaim, token string, delivery deliveryEnvelope) error {
	input, toolCount, err := w.modelInput(ctx, claim, delivery)
	if err != nil {
		return err
	}
	if len(input.Message.Metadata) != 0 {
		root := cloudharness.ThreadID(claim.Lease.Ref.SessionUUID)
		if err := w.database.HarnessState(claim.Lease).SetMetadata(ctx, root, input.Message.Metadata); err != nil {
			return err
		}
	}
	var available []fantasy.Tool
	if toolCount < db.HarnessMaxToolCalls {
		available, err = w.modelTools(ctx, claim)
		if err != nil {
			return err
		}
	}
	conversation := w.database.HarnessConversation(claim.Lease)
	threadID := input.Message.GroupID
	history, _, _, err := conversation.LoadHistoryWithAncestors(ctx, threadID)
	if err != nil {
		return err
	}
	present, err := conversation.HasMessageID(ctx, threadID, input.DedupID)
	if err != nil {
		return err
	}
	if !present {
		message, ok := input.Message.Content.(cloudharness.Message)
		if !ok {
			return fmt.Errorf("invalid harness model input %T", input.Message.Content)
		}
		history = append(history, message)
	}
	model, err := w.model(ctx, claim, token, available)
	if err != nil {
		return err
	}
	if _, err := model.Complete(ctx, history); err != nil {
		return fmt.Errorf("harness text model step failed: %w", err)
	}
	return nil
}

func (w *Worker) modelInput(ctx context.Context, claim db.HarnessTurnClaim, delivery deliveryEnvelope) (cloudharness.StepInput, int, error) {
	threadID := delivery.Turn.SessionUUID
	if claim.Subagent != nil {
		threadID = claim.Subagent.UUID
		if claim.Task.Kind == db.HarnessSubagentTask {
			prompt, err := w.database.HarnessDelegationPrompt(ctx, claim.Lease, claim.CodeSession.SessionExternalID)
			return cloudharness.StepInput{
				Message: cloudharness.InputMessage{GroupID: cloudharness.ThreadID(threadID), Content: cloudharness.UserMessage{Text: prompt}},
				DedupID: claim.Lease.DelegationToolUUID,
			}, 0, err
		}
	}
	if claim.Task.Kind == db.HarnessModelTask {
		continuation, err := w.loadContinuation(ctx, claim.Task)
		if err != nil {
			return cloudharness.StepInput{}, 0, err
		}
		return continuation.stepInput(threadID, claim.Task.ToolUUID), continuation.ToolCount, nil
	}
	input, err := delivery.stepInput(claim.LegacyInput, claim.InputEventUUID)
	return input, 0, err
}

func (w *Worker) model(ctx context.Context, claim db.HarnessTurnClaim, token string, tools []fantasy.Tool) (*fantasyadapter.FantasyModel, error) {
	modelID, prompt, err := codesessions.HarnessModelConfiguration(claim)
	if err != nil {
		return nil, err
	}
	provider, err := anthropic.New(
		anthropic.WithBaseURL(w.apiURL), anthropic.WithAPIKey(token),
		anthropic.WithHTTPClient(llmproviders.NewHTTPClient(0)),
		anthropic.WithHeaders(map[string]string{sessioncontract.HarnessTurnHeader: claim.Lease.Ref.UUID}),
	)
	if err != nil {
		return nil, errors.New("configure harness model provider")
	}
	model, err := provider.LanguageModel(ctx, modelID)
	if err != nil {
		return nil, errors.New("configure harness session model")
	}
	reminder, err := codesessions.HarnessTimeReminder(time.Now())
	if err != nil {
		return nil, err
	}
	return fantasyadapter.NewModel(model, tools).
		WithSystemPrompt(prompt).
		WithRuntimeReminder(reminder).
		WithProviderOptions(fantasy.ProviderOptions{
			anthropic.Name: &anthropic.ProviderOptions{DisableParallelToolUse: new(true)},
		}), nil
}

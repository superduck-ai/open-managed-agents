package db

import (
	"context"

	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/yourbatis"
)

func applyHarnessSubscriptionTool(ctx context.Context, executor yourbatis.Executor, ref HarnessTurnRef, tool HarnessTool) error {
	if tool.Name != sessioncontract.HarnessSubscribe && tool.Name != sessioncontract.HarnessCancelSubscription {
		return nil
	}
	key := harnessRootKey(ref)
	state, err := lockHarnessThreadState(ctx, executor, key)
	if err != nil {
		return err
	}
	if tool.Name == sessioncontract.HarnessSubscribe {
		if state.Subscriptions == nil {
			state.Subscriptions = make(map[harness.ToolCallID]struct{})
		}
		state.Subscriptions[harness.ToolCallID(tool.UUID)] = struct{}{}
	} else {
		id, err := sessioncontract.ParseHarnessSubscriptionID(tool.Arguments)
		if err != nil {
			return err
		}
		delete(state.Subscriptions, harness.ToolCallID(id))
	}
	return saveHarnessState(ctx, NewHarnessStateMapper(executor), key, state)
}

func acceptHarnessSubscriptionEventTx(ctx context.Context, executor yourbatis.Executor, session Session, event SessionEvent) error {
	input, err := sessioncontract.ParseHarnessUserInput(event.Payload)
	if err != nil || input.Subscription == nil {
		return err
	}
	key := harnessRootKey(HarnessTurnRef{OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID})
	state, err := lockHarnessThreadState(ctx, executor, key)
	if err != nil {
		if err == ErrNotFound {
			return ErrSessionInputConflict
		}
		return err
	}
	id := harness.ToolCallID(input.Subscription.ID)
	if _, active := state.Subscriptions[id]; !active {
		return ErrSessionInputConflict
	}
	if !input.Subscription.Final {
		return nil
	}
	delete(state.Subscriptions, id)
	return saveHarnessState(ctx, NewHarnessStateMapper(executor), key, state)
}

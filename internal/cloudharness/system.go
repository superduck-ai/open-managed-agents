package cloudharness

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Model interface {
	Complete(context.Context, []Message) (ModelCompleted, error)
}

type ToolExecutor interface {
	Execute(context.Context, ToolCall) ToolResult
}

type AgentSystem struct {
	stepMu       sync.Mutex
	conversation ConversationStore
	stateStore   ExecutionStateStore
	model        Model
	tools        ToolExecutor
	sender       InputSender
}

type threadBatch struct {
	id     ThreadID
	inputs []StepInput
	state  State
	seen   map[string]struct{}
}

func NewAgentSystem(conversation ConversationStore, stateStore ExecutionStateStore, model Model, tools ToolExecutor, sender InputSender) *AgentSystem {
	return &AgentSystem{conversation: conversation, stateStore: stateStore, model: model, tools: tools, sender: sender}
}

func (s *AgentSystem) Sender() InputSender {
	return s.sender
}

func (s *AgentSystem) Step(ctx context.Context, inputs []StepInput, observer ThreadObserver, deferQueue DeferQueue) ([]ThreadStepOutcome, error) {
	s.stepMu.Lock()
	defer s.stepMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	groups := groupStepInputs(inputs)
	for index, group := range groups {
		ready, err := s.loadBatch(ctx, group, deferQueue)
		if err != nil {
			return nil, fmt.Errorf("load thread %s batch: %w", group.id, err)
		}
		groups[index] = ready
	}

	var observerMu sync.Mutex
	notify := func(threadID ThreadID, event AgentEvent) {
		if observer != nil {
			observerMu.Lock()
			defer observerMu.Unlock()
			observer.OnEvent(threadID, event)
		}
	}
	outcomes := make([]ThreadStepOutcome, len(groups))
	failures := make([]error, len(groups))
	var steps sync.WaitGroup
	for index, group := range groups {
		steps.Go(func() {
			outcome, err := s.stepThread(ctx, group, notify)
			outcomes[index] = ThreadStepOutcome{ThreadID: group.id, Outcome: outcome}
			failures[index] = err
		})
	}
	steps.Wait()
	for index, err := range failures {
		if err != nil {
			return nil, fmt.Errorf("step thread %s: %w", groups[index].id, err)
		}
	}
	return outcomes, nil
}

func (s *AgentSystem) loadBatch(ctx context.Context, group threadBatch, deferQueue DeferQueue) (threadBatch, error) {
	state, err := s.stateStore.LoadState(ctx, group.id)
	if err != nil {
		return threadBatch{}, err
	}
	group.state = state
	group.seen = make(map[string]struct{})
	group.inputs, err = s.filterDeferrable(ctx, group, deferQueue)
	return group, err
}

func groupStepInputs(inputs []StepInput) []threadBatch {
	indexByThread := make(map[ThreadID]int)
	var groups []threadBatch
	for _, input := range inputs {
		threadID := input.Message.GroupID
		index, exists := indexByThread[threadID]
		if !exists {
			index = len(groups)
			indexByThread[threadID] = index
			groups = append(groups, threadBatch{id: threadID})
		}
		groups[index].inputs = append(groups[index].inputs, input)
	}
	return groups
}

func (s *AgentSystem) stepThread(ctx context.Context, group threadBatch, notify func(ThreadID, AgentEvent)) (StepOutcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(group.inputs) == 0 {
		return Skipped{}, nil
	}
	state, err := s.prepareInputs(ctx, group)
	if err != nil {
		return nil, err
	}
	outcome, state, err := s.completeModel(ctx, group.id, state, notify)
	if err != nil {
		return nil, err
	}
	switch work := state.Pending.(type) {
	case ApprovalWork:
		notify(group.id, AgentEvent{Kind: ToolApprovalRequested, Call: work.Call})
	case ToolWork:
		if err := s.dispatchTool(ctx, group.id, state, work, notify); err != nil {
			return nil, err
		}
	}
	return outcome, nil
}

func (s *AgentSystem) prepareInputs(ctx context.Context, group threadBatch) (State, error) {
	state := group.state
	for _, input := range group.inputs {
		if err := s.storeInputMetadata(ctx, group.id, input.Message); err != nil {
			return State{}, err
		}
		dedupID := input.DedupID
		if group.isProcessed(dedupID) {
			continue
		}
		next, err := s.commitInput(ctx, group.id, state, input)
		if err != nil {
			return State{}, err
		}
		if next.Version != state.Version && dedupID != "" {
			group.seen[dedupID] = struct{}{}
		}
		state = next
	}
	return state, nil
}

func (s *AgentSystem) storeInputMetadata(ctx context.Context, threadID ThreadID, message InputMessage) error {
	if len(message.Metadata) == 0 {
		return nil
	}
	if _, user := message.Content.(UserMessage); user {
		if err := s.conversation.EnsureRootThread(ctx, threadID); err != nil {
			return err
		}
	}
	ancestors, err := s.conversation.GetAncestorChain(ctx, threadID)
	if err != nil {
		return err
	}
	if len(ancestors) != 0 {
		threadID = ancestors[0].ThreadID
	}
	return s.stateStore.SetMetadata(ctx, threadID, message.Metadata)
}

func (group threadBatch) isProcessed(dedupID string) bool {
	_, exists := group.seen[dedupID]
	return dedupID != "" && exists
}

func (s *AgentSystem) commitInput(ctx context.Context, threadID ThreadID, state State, input StepInput) (State, error) {
	content := input.Message.Content
	switch content.(type) {
	case ResumeThread:
		return state, nil
	case UserMessage:
		if err := s.conversation.EnsureRootThread(ctx, threadID); err != nil {
			return State{}, err
		}
	case Approval, ToolResult:
	default:
		return State{}, fmt.Errorf("%w: unsupported batch input %T", ErrUnexpectedInput, content)
	}
	transition, err := Step(state, content)
	if err != nil {
		if _, isText := content.(UserMessage); !isText && errors.Is(err, ErrUnexpectedInput) {
			return state, nil
		}
		return State{}, err
	}
	return s.stateStore.Commit(ctx, threadID, state.Version, transition, input.DedupID)
}

func (s *AgentSystem) completeModel(ctx context.Context, threadID ThreadID, state State, notify func(ThreadID, AgentEvent)) (StepOutcome, State, error) {
	if _, waiting := state.Pending.(ModelWork); !waiting {
		return Skipped{}, state, nil
	}
	if s.model == nil {
		return nil, State{}, errors.New("model is required for pending completion")
	}
	history, _, _, err := s.conversation.LoadHistoryWithAncestors(ctx, threadID)
	if err != nil {
		return nil, State{}, err
	}
	notify(threadID, AgentEvent{Kind: CompletionStarted})
	result, err := s.model.Complete(ctx, history)
	if err != nil {
		return nil, State{}, fmt.Errorf("complete model work: %w", err)
	}
	if !result.Persisted {
		state, err = s.apply(ctx, threadID, state, result, "")
		if err != nil {
			return nil, State{}, err
		}
	}
	notify(threadID, AgentEvent{Kind: CompletionFinished, Usage: result.Usage})
	return Completed{Usage: result.Usage, ContextWindow: result.ContextWindow}, state, nil
}

func (s *AgentSystem) dispatchTool(ctx context.Context, threadID ThreadID, state State, work ToolWork, notify func(ThreadID, AgentEvent)) error {
	if s.Sender() == nil {
		return errors.New("input sender is required for tool results")
	}
	if work.Result.CallID == "" {
		if s.tools == nil {
			return errors.New("tool executor is required for pending tool work")
		}
		notify(threadID, AgentEvent{Kind: ToolExecutionStarted, Call: work.Call})
		work.Result = s.tools.Execute(ctx, work.Call)
		if _, err := s.apply(ctx, threadID, state, toolExecuted{Result: work.Result}, ""); err != nil {
			return err
		}
	}
	message := InputMessage{GroupID: threadID, Content: work.Result}
	if err := s.Sender().SendToInputQueue(ctx, message, "tool-result:"+string(work.Call.ID)); err != nil {
		return fmt.Errorf("send tool result: %w", err)
	}
	return nil
}

func (s *AgentSystem) apply(ctx context.Context, threadID ThreadID, state State, input Input, dedupID string) (State, error) {
	transition, err := Step(state, input)
	if err != nil {
		return State{}, err
	}
	return s.stateStore.Commit(ctx, threadID, state.Version, transition, dedupID)
}

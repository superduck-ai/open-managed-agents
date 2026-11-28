package cloudharness

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
)

var ErrUnexpectedInput = errors.New("input does not match the pending work")

type Work interface {
	pendingWork()
}

type ModelWork struct {
	AutoExecuteTools bool
	UserChoiceTool   string
}
type ApprovalWork struct{ Call ToolCall }
type UserChoiceWork struct{ Call ToolCall }
type ToolWork struct {
	Call   ToolCall
	Result ToolResult
}

func (ModelWork) pendingWork()      {}
func (ApprovalWork) pendingWork()   {}
func (UserChoiceWork) pendingWork() {}
func (ToolWork) pendingWork()       {}

type Transition struct {
	Pending  Work
	Messages []Message
}

type StepOutcome interface {
	stepOutcome()
}

type Skipped struct{}

type Completed struct {
	Usage         *Usage
	ContextWindow uint
}

func (Skipped) stepOutcome()   {}
func (Completed) stepOutcome() {}

type threadExecution struct {
	system   *AgentSystem
	id       ThreadID
	inputs   []StepInput
	pending  Work
	seen     map[string]struct{}
	buffered []MessageWithID
	deferred []StepInput
}

func (t *threadExecution) load(ctx context.Context, deferQueue DeferQueue) error {
	if t.system.task != nil {
		t.pending = t.system.task.Work
		return nil
	}
	t.pending = t.system.memory.readThread(t.id).pending
	t.seen = make(map[string]struct{})
	inputs, err := t.filterDeferrable(ctx, deferQueue)
	t.inputs = inputs
	return err
}

func (t *threadExecution) step(ctx context.Context, notify func(ThreadID, AgentEvent)) (StepOutcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(t.inputs) == 0 {
		return Skipped{}, nil
	}
	pending, err := t.prepareInputs(ctx)
	if err != nil {
		return nil, err
	}
	outcome, pending, err := t.completeModel(ctx, pending, notify)
	if err != nil {
		return nil, err
	}
	switch work := pending.(type) {
	case ApprovalWork:
		notify(t.id, AgentEvent{Kind: ToolApprovalRequested, Call: work.Call})
	case ToolWork:
		if _, completed := outcome.(Completed); completed && t.system.task != nil {
			return outcome, nil
		}
		if err := t.dispatchTool(ctx, work, notify); err != nil {
			return nil, err
		}
	}
	return outcome, nil
}

func (t *threadExecution) prepareInputs(ctx context.Context) (Work, error) {
	if t.system.task != nil {
		return t.prepareClaimedInput(t.inputs[0].Message.Content)
	}
	pending := t.pending
	for _, input := range t.inputs {
		if err := t.storeInputMetadata(ctx, input.Message); err != nil {
			return nil, err
		}
		if t.isProcessed(input.DedupID) {
			continue
		}
		next, err := t.prepareInput(ctx, pending, input)
		if err != nil {
			return nil, err
		}
		pending = next
	}
	return pending, nil
}

func (t *threadExecution) prepareClaimedInput(input Input) (Work, error) {
	switch event := input.(type) {
	case ResumeThread:
		switch t.pending.(type) {
		case ModelWork, ToolWork:
			return t.pending, nil
		default:
			return nil, fmt.Errorf("%w: unsupported claimed work %T", ErrUnexpectedInput, t.pending)
		}
	case UserMessage:
		if event.Text != "" {
			return t.system.task.ModelPolicy, nil
		}
	case ToolResult:
		if matchesToolResult(t.pending, event.CallID) {
			return t.system.task.ModelPolicy, nil
		}
	}
	return nil, fmt.Errorf("%w: %T while waiting on %T", ErrUnexpectedInput, input, t.pending)
}

func (t *threadExecution) storeInputMetadata(ctx context.Context, message InputMessage) error {
	if len(message.Metadata) == 0 {
		return nil
	}
	threadID := t.id
	if _, user := message.Content.(UserMessage); user {
		if err := t.system.memory.EnsureRootThread(ctx, threadID); err != nil {
			return err
		}
	}
	ancestors, err := t.system.memory.GetAncestorChain(ctx, threadID)
	if err != nil {
		return err
	}
	if len(ancestors) != 0 {
		threadID = ancestors[0].ThreadID
	}
	return t.system.memory.SetMetadata(ctx, threadID, message.Metadata)
}

func (t *threadExecution) isProcessed(dedupID string) bool {
	if dedupID == "" {
		return false
	}
	_, exists := t.seen[dedupID]
	return exists || t.system.memory.hasMessageID(t.id, dedupID)
}

func (t *threadExecution) prepareInput(ctx context.Context, pending Work, input StepInput) (Work, error) {
	var next Work = ModelWork{}
	var messages []Message
	switch content := input.Message.Content.(type) {
	case ResumeThread:
		return pending, nil
	case UserMessage:
		if err := t.system.memory.EnsureRootThread(ctx, t.id); err != nil {
			return nil, err
		}
		if content.Text == "" {
			return nil, fmt.Errorf("%w: %T while waiting on %T", ErrUnexpectedInput, content, pending)
		}
		if callID, waiting := waitingForCall(pending); waiting && callID != "" {
			messages = append(messages, ToolResult{CallID: callID, Text: "The user interrupted this call.", Failed: true})
		}
		messages = append(messages, content)
	case Approval:
		work, waiting := pending.(ApprovalWork)
		if !waiting || content.CallID != work.Call.ID {
			return pending, nil
		}
		if content.Granted {
			next = ToolWork{Call: work.Call}
		} else {
			messages = append(messages, ToolResult{CallID: work.Call.ID, Text: "The user denied this call.", Failed: true})
		}
	case ToolResult:
		if !matchesToolResult(pending, content.CallID) {
			return pending, nil
		}
		messages = append(messages, content)
	default:
		return nil, fmt.Errorf("%w: unsupported batch input %T", ErrUnexpectedInput, content)
	}
	t.stageMessages(messages, input.DedupID)
	if input.DedupID != "" {
		t.seen[input.DedupID] = struct{}{}
	}
	return next, nil
}

func matchesToolResult(pending Work, callID ToolCallID) bool {
	work, waiting := pending.(ToolWork)
	return waiting && callID == work.Call.ID
}

func (t *threadExecution) stageMessages(messages []Message, inputID string) {
	for index, message := range messages {
		id := inputID
		if id == "" || index != len(messages)-1 {
			id = rand.Text()
		}
		t.buffered = append(t.buffered, MessageWithID{Message: message, ID: id})
	}
}

func (t *threadExecution) completeModel(ctx context.Context, pending Work, notify func(ThreadID, AgentEvent)) (StepOutcome, Work, error) {
	work, waiting := pending.(ModelWork)
	if !waiting {
		return Skipped{}, pending, nil
	}
	result, err := t.runModel(ctx, notify)
	if err != nil {
		return nil, nil, fmt.Errorf("complete model work: %w", err)
	}
	next, err := t.commitModel(ctx, work, result.Message)
	if err != nil {
		return nil, nil, err
	}
	notify(t.id, AgentEvent{Kind: CompletionFinished, Usage: result.Usage})
	return Completed{Usage: result.Usage, ContextWindow: result.ContextWindow}, next, nil
}

func (t *threadExecution) runModel(ctx context.Context, notify func(ThreadID, AgentEvent)) (ModelCompleted, error) {
	if t.system.task != nil {
		if t.system.task.CompleteModel == nil || t.system.task.CommitModel == nil {
			return ModelCompleted{}, errors.New("claimed model task requires completion and commit")
		}
		notify(t.id, AgentEvent{Kind: CompletionStarted})
		return t.system.task.CompleteModel(ctx)
	}
	if t.system.model == nil {
		return ModelCompleted{}, errors.New("model is required for pending completion")
	}
	history, _, _, err := t.system.memory.LoadHistoryWithAncestors(ctx, t.id)
	if err != nil {
		return ModelCompleted{}, err
	}
	for _, message := range t.buffered {
		history = append(history, message.Message)
	}
	notify(t.id, AgentEvent{Kind: CompletionStarted})
	return t.system.model.Complete(ctx, history)
}

func (t *threadExecution) commitModel(ctx context.Context, work ModelWork, message AssistantMessage) (Work, error) {
	transition, err := modelTransition(work, message)
	if err != nil {
		return nil, err
	}
	if task := t.system.task; task != nil {
		if err := task.CommitModel(ctx, transition); err != nil {
			return nil, err
		}
	} else {
		t.stageMessages(transition.Messages, "")
		if err := t.system.memory.AppendMessages(ctx, t.id, t.buffered); err != nil {
			return nil, err
		}
		t.system.memory.setPending(t.id, transition.Pending)
	}
	return transition.Pending, nil
}

func modelTransition(work ModelWork, message AssistantMessage) (Transition, error) {
	transition := Transition{Messages: []Message{message}}
	call := message.Call
	switch {
	case call == (ToolCall{}) && message.Text != "":
	case call.ID == "" || call.Name == "":
		return Transition{}, fmt.Errorf("%w: incomplete model response", ErrUnexpectedInput)
	case call.Name == work.UserChoiceTool:
		transition.Pending = UserChoiceWork{Call: call}
	case work.AutoExecuteTools:
		transition.Pending = ToolWork{Call: call}
	default:
		transition.Pending = ApprovalWork{Call: call}
	}
	return transition, nil
}

func (t *threadExecution) dispatchTool(ctx context.Context, work ToolWork, notify func(ThreadID, AgentEvent)) error {
	if t.system.task != nil {
		if t.system.task.DispatchTool == nil {
			return errors.New("claimed tool task requires dispatch")
		}
		notify(t.id, AgentEvent{Kind: ToolExecutionStarted, Call: work.Call})
		return t.system.task.DispatchTool(ctx, work)
	}
	t.system.memory.setPending(t.id, work)
	if t.system.Sender() == nil {
		return errors.New("input sender is required for tool results")
	}
	if work.Result.CallID == "" {
		if t.system.tools == nil {
			return errors.New("tool executor is required for pending tool work")
		}
		notify(t.id, AgentEvent{Kind: ToolExecutionStarted, Call: work.Call})
		work.Result = t.system.tools.Execute(ctx, work.Call)
		if work.Result.CallID != work.Call.ID {
			return fmt.Errorf("%w: tool result %q does not match call %q", ErrUnexpectedInput, work.Result.CallID, work.Call.ID)
		}
		t.system.memory.setPending(t.id, work)
	}
	message := InputMessage{GroupID: t.id, Content: work.Result}
	if err := t.system.Sender().SendToInputQueue(ctx, message, "tool-result:"+string(work.Call.ID)); err != nil {
		return fmt.Errorf("send tool result: %w", err)
	}
	return nil
}

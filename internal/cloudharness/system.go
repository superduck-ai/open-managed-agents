package cloudharness

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type ToolExecutor interface {
	Execute(context.Context, ToolCall) ToolResult
}

type ClaimedTask struct {
	Work          Work
	ModelPolicy   ModelWork
	CompleteModel func(context.Context) (ModelCompleted, error)
	CommitModel   func(context.Context, Transition) error
	DispatchTool  func(context.Context, ToolWork) error
}

type StepInput struct {
	Message InputMessage
	DedupID string
}

type ThreadStepOutcome struct {
	ThreadID ThreadID
	Outcome  StepOutcome
}

type AgentSystem struct {
	stepMu sync.Mutex
	memory *MemoryStore
	model  Model
	tools  ToolExecutor
	sender InputSender
	task   *ClaimedTask
}

func NewAgentSystem(store *MemoryStore, model Model, tools ToolExecutor, sender InputSender) *AgentSystem {
	return &AgentSystem{memory: store, model: model, tools: tools, sender: sender}
}

func NewClaimedAgentSystem(task ClaimedTask) *AgentSystem {
	return &AgentSystem{task: &task}
}

func (s *AgentSystem) Sender() InputSender {
	return s.sender
}

func (s *AgentSystem) Step(ctx context.Context, inputs []StepInput, observer ThreadObserver, deferQueue DeferQueue) (outcomes []ThreadStepOutcome, err error) {
	stepMu := &s.stepMu
	if s.memory != nil {
		stepMu = &s.memory.stepMu
	}
	stepMu.Lock()
	defer stepMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.task != nil && (len(inputs) != 1 || deferQueue != nil) {
		return nil, fmt.Errorf("%w: expected one claimed task", ErrUnexpectedInput)
	}
	threads := s.groupStepInputs(inputs)
	if deferQueue != nil {
		defer func() {
			err = errors.Join(err, restoreDeferred(context.WithoutCancel(ctx), threads, deferQueue))
		}()
	}
	for _, thread := range threads {
		if err := thread.load(ctx, deferQueue); err != nil {
			return nil, fmt.Errorf("load thread %s batch: %w", thread.id, err)
		}
	}

	var observerMu sync.Mutex
	notify := func(threadID ThreadID, event AgentEvent) {
		if observer != nil {
			observerMu.Lock()
			defer observerMu.Unlock()
			observer.OnEvent(threadID, event)
		}
	}
	outcomes = make([]ThreadStepOutcome, len(threads))
	failures := make([]error, len(threads))
	var steps sync.WaitGroup
	for index, thread := range threads {
		steps.Go(func() {
			outcome, err := thread.step(ctx, notify)
			if err == nil {
				thread.deferred = nil
			}
			outcomes[index] = ThreadStepOutcome{ThreadID: thread.id, Outcome: outcome}
			failures[index] = err
		})
	}
	steps.Wait()
	for index, err := range failures {
		if err != nil {
			return nil, fmt.Errorf("step thread %s: %w", threads[index].id, err)
		}
	}
	return outcomes, nil
}

func (s *AgentSystem) groupStepInputs(inputs []StepInput) []*threadExecution {
	indexByThread := make(map[ThreadID]int)
	var threads []*threadExecution
	for _, input := range inputs {
		threadID := input.Message.GroupID
		index, exists := indexByThread[threadID]
		if !exists {
			index = len(threads)
			indexByThread[threadID] = index
			threads = append(threads, &threadExecution{system: s, id: threadID})
		}
		threads[index].inputs = append(threads[index].inputs, input)
	}
	return threads
}

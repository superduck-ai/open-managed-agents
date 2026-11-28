package cloudharness

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

type DeferQueue interface {
	Push(ctx context.Context, input InputMessage, messageID string) (bool, error)
	Drain(ctx context.Context, threadID ThreadID) ([]StepInput, error)
}

type MemoryDeferQueue struct {
	mu     sync.Mutex
	inputs map[ThreadID][]StepInput
}

func (q *MemoryDeferQueue) Push(_ context.Context, input InputMessage, messageID string) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.inputs == nil {
		q.inputs = make(map[ThreadID][]StepInput)
	}
	q.inputs[input.GroupID] = append(q.inputs[input.GroupID], StepInput{Message: input, DedupID: messageID})
	return true, nil
}

func (q *MemoryDeferQueue) Drain(_ context.Context, threadID ThreadID) ([]StepInput, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	inputs := q.inputs[threadID]
	delete(q.inputs, threadID)
	return inputs, nil
}

func (t *threadExecution) filterDeferrable(ctx context.Context, queue DeferQueue) ([]StepInput, error) {
	if queue == nil {
		return t.inputs, nil
	}
	callID, waiting := waitingForCall(t.pending)
	if !waiting || slices.ContainsFunc(t.inputs, func(input StepInput) bool {
		return !t.isProcessed(input.DedupID) && settlesCall(input.Message, t.pending, callID)
	}) {
		deferred, err := queue.Drain(ctx, t.id)
		t.deferred = deferred
		return append(t.inputs, deferred...), err
	}
	var ready []StepInput
	for _, input := range t.inputs {
		if input.Message.Synthetic && !t.isProcessed(input.DedupID) {
			queued, err := queue.Push(ctx, input.Message, input.DedupID)
			if err != nil {
				return nil, err
			}
			if queued {
				continue
			}
		}
		ready = append(ready, input)
	}
	return ready, nil
}

func restoreDeferred(ctx context.Context, threads []*threadExecution, queue DeferQueue) error {
	var restoreErr error
	for _, thread := range threads {
		for _, input := range thread.deferred {
			queued, err := queue.Push(ctx, input.Message, input.DedupID)
			if err == nil && !queued {
				err = fmt.Errorf("defer queue rejected input %q", input.DedupID)
			}
			if err != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("restore deferred input for thread %s: %w", thread.id, err))
			}
		}
	}
	return restoreErr
}

func waitingForCall(work Work) (ToolCallID, bool) {
	switch value := work.(type) {
	case ApprovalWork:
		return value.Call.ID, true
	case ToolWork:
		return value.Call.ID, true
	default:
		return "", false
	}
}

func settlesCall(message InputMessage, work Work, callID ToolCallID) bool {
	if message.Synthetic {
		return false
	}
	switch value := message.Content.(type) {
	case UserMessage:
		return value.Text != ""
	case Approval:
		_, waiting := work.(ApprovalWork)
		return waiting && !value.Granted && value.CallID == callID
	case ToolResult:
		return matchesToolResult(work, value.CallID)
	default:
		return false
	}
}

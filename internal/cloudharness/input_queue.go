package cloudharness

import (
	"context"
	"errors"
	"slices"
	"sync"
)

type MemoryInputQueue struct {
	deliveryMu sync.Mutex
	mu         sync.Mutex
	inputs     []StepInput
}

func (q *MemoryInputQueue) SendToInputQueue(ctx context.Context, message InputMessage, dedupID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.enqueue(StepInput{Message: message, DedupID: dedupID})
	return nil
}

func (q *MemoryInputQueue) enqueue(next StepInput) {
	queued := slices.ContainsFunc(q.inputs, func(input StepInput) bool {
		return input.Message.GroupID == next.Message.GroupID && input.DedupID == next.DedupID
	})
	if next.DedupID == "" || !queued {
		q.inputs = append(q.inputs, next)
	}
}

func (q *MemoryInputQueue) DeliverPending(ctx context.Context, maxBatches int, handle func(context.Context, []StepInput) error) error {
	q.deliveryMu.Lock()
	defer q.deliveryMu.Unlock()
	for delivered := 0; ; delivered++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch := q.take()
		if len(batch) == 0 {
			return nil
		}
		if delivered >= maxBatches {
			q.redeliver(batch)
			return errors.New("memory input queue exceeded its delivery limit")
		}
		if err := handle(ctx, batch); err != nil {
			q.redeliver(batch)
			return err
		}
	}
}

func (q *MemoryInputQueue) take() []StepInput {
	q.mu.Lock()
	defer q.mu.Unlock()
	inputs := q.inputs
	q.inputs = nil
	return inputs
}

func (q *MemoryInputQueue) redeliver(batch []StepInput) {
	q.mu.Lock()
	defer q.mu.Unlock()
	pending := q.inputs
	q.inputs = slices.Clone(batch)
	for _, input := range pending {
		q.enqueue(input)
	}
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

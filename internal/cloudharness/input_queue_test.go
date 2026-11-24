package cloudharness

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestMemoryInputQueueRedeliversFailedBatchBeforeNewInput(t *testing.T) {
	ctx := t.Context()
	queue := &MemoryInputQueue{}
	initial := InputMessage{GroupID: "thread", Content: UserMessage{Text: "first"}}
	if err := queue.SendToInputQueue(ctx, initial, "first"); err != nil {
		t.Fatal(err)
	}
	handlerError := errors.New("handler interrupted")
	err := queue.DeliverPending(ctx, 1, func(ctx context.Context, _ []StepInput) error {
		next := InputMessage{GroupID: "thread", Content: UserMessage{Text: "second"}}
		if err := queue.SendToInputQueue(ctx, next, "second"); err != nil {
			return err
		}
		return handlerError
	})
	if !errors.Is(err, handlerError) {
		t.Fatalf("got %v, want handler error", err)
	}
	var ids []string
	err = queue.DeliverPending(ctx, 1, func(_ context.Context, batch []StepInput) error {
		for _, input := range batch {
			ids = append(ids, input.DedupID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []string{"first", "second"}) {
		t.Fatalf("redelivery IDs = %v, want [first second]", ids)
	}
}

package cloudharness

import (
	"context"
	"slices"
)

func (s *AgentSystem) filterDeferrable(ctx context.Context, group threadBatch, queue DeferQueue) ([]StepInput, error) {
	if queue == nil {
		return group.inputs, nil
	}
	callID, waiting := waitingForCall(group.state.Pending)
	if waiting {
		if err := s.findRecordedInputs(ctx, group); err != nil {
			return nil, err
		}
	}
	if !waiting || slices.ContainsFunc(group.inputs, func(input StepInput) bool {
		return !group.isProcessed(input.DedupID) && settlesCall(input.Message, group.state.Pending, callID)
	}) {
		deferred, err := queue.Drain(ctx, group.id)
		return append(group.inputs, deferred...), err
	}
	var ready []StepInput
	for _, input := range group.inputs {
		if input.Message.Synthetic && !group.isProcessed(input.DedupID) {
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

func (s *AgentSystem) findRecordedInputs(ctx context.Context, group threadBatch) error {
	for _, input := range group.inputs {
		if input.DedupID == "" || group.isProcessed(input.DedupID) {
			continue
		}
		found, err := s.stateStore.HasAppliedInput(ctx, group.id, input.DedupID)
		if err != nil {
			return err
		}
		if found {
			group.seen[input.DedupID] = struct{}{}
		}
	}
	return nil
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
		_, waiting := work.(ToolWork)
		return waiting && value.CallID == callID
	default:
		return false
	}
}

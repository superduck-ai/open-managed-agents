package db

import "context"

func (tx ManagedAgentEventTx) FindWorkerEventReceipt(ctx context.Context, worker CodeSession, eventID string) (WorkerEventReceipt, bool, error) {
	return NewWorkerEventReceiptMapper(tx.executor).Find(ctx, worker.WorkspaceUUID, worker.UUID, worker.CurrentWorkerEpoch, eventID)
}

func (tx ManagedAgentEventTx) InsertWorkerEventReceipt(ctx context.Context, worker CodeSession, receipt WorkerEventReceipt) error {
	_, err := NewWorkerEventReceiptMapper(tx.executor).Insert(ctx, worker.WorkspaceUUID, worker.UUID, worker.CurrentWorkerEpoch, receipt)
	return err
}

func (tx ManagedAgentEventTx) LatestWorkerRequestBlockIndices(ctx context.Context, worker CodeSession, requestIDs []string) ([]WorkerRequestBlockIndex, error) {
	if len(requestIDs) == 0 {
		return nil, nil
	}
	return NewWorkerEventReceiptMapper(tx.executor).LatestBlockIndices(ctx, worker.WorkspaceUUID, worker.UUID, worker.CurrentWorkerEpoch, requestIDs)
}

package eventpayload

import (
	"context"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// RestoreInternal exposes the existing blob boundary to archival without changing resume reads.
func (s *Store) RestoreInternal(ctx context.Context, event db.CodeSessionInternalEvent) (db.CodeSessionInternalEvent, error) {
	payload, err := s.restore(ctx, event.WorkspaceUUID, event.Payload, event.PayloadBlobUUID)
	if err != nil {
		return db.CodeSessionInternalEvent{}, err
	}
	event.Payload = payload
	return event, nil
}

func (s *Store) RestoreInternalTx(ctx context.Context, tx db.ManagedAgentEventTx, event db.CodeSessionInternalEvent) (db.CodeSessionInternalEvent, error) {
	payload, err := s.restoreTx(ctx, tx, event.WorkspaceUUID, event.Payload, event.PayloadBlobUUID)
	if err != nil {
		return db.CodeSessionInternalEvent{}, err
	}
	event.Payload = payload
	return event, nil
}

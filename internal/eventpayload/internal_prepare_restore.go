package eventpayload

import (
	"context"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// PrepareInternalRestore recreates blob storage after GC without altering worker hash metadata.
func (s *Store) PrepareInternalRestore(ctx context.Context, event db.CodeSessionInternalEvent) (db.CodeSessionInternalEvent, error) {
	summary, _, err := Summarize(event.Payload, event.EventType)
	if err != nil {
		return db.CodeSessionInternalEvent{}, err
	}
	event.Payload, event.PayloadBlobUUID, err = s.prepare(ctx, event.OrganizationUUID, event.WorkspaceUUID, "internal/"+event.IdempotencyKey, event.Payload, summary)
	return event, err
}

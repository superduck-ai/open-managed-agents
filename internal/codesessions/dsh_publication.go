package codesessions

import (
	"context"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type dshPublicationStore interface {
	ValidateDSHPreviewWorker(context.Context, db.DSHScope) error
	PublishDSHEvents(context.Context, db.DSHScope, []db.DSHPublicEvent) (db.DSHPublishResult, error)
}

func publishDSHBatch(ctx context.Context, store dshPublicationStore, scope db.DSHScope, batch []db.DSHPublicEvent) (db.DSHPublishResult, error) {
	if len(batch) == 0 {
		return db.DSHPublishResult{}, store.ValidateDSHPreviewWorker(ctx, scope)
	}
	return store.PublishDSHEvents(ctx, scope, batch)
}

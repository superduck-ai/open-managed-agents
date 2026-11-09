package codesessions

import (
	"context"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

type previewPublicationStore struct {
	validationErr error
	validations   int
	publications  int
	scope         db.DSHScope
}

func (s *previewPublicationStore) ValidateDSHPreviewWorker(_ context.Context, scope db.DSHScope) error {
	s.validations++
	s.scope = scope
	return s.validationErr
}
func (s *previewPublicationStore) PublishDSHEvents(_ context.Context, scope db.DSHScope, batch []db.DSHPublicEvent) (db.DSHPublishResult, error) {
	s.publications++
	s.scope = scope
	return db.DSHPublishResult{Accepted: len(batch)}, nil
}

func TestPreviewPublicationRejectsInvalidWorkerWithoutPersistentTransaction(t *testing.T) {
	for _, failure := range []error{db.ErrWorkerEpochMismatch, db.ErrWorkerLeaseExpired, db.ErrNotFound} {
		store := &previewPublicationStore{validationErr: failure}
		_, err := publishDSHBatch(t.Context(), store, db.DSHScope{Epoch: 7}, nil)
		if !errors.Is(err, failure) || store.validations != 1 || store.publications != 0 {
			t.Fatalf("err=%v store=%+v", err, store)
		}
	}
}
func TestPreviewPublicationSkipsPersistentTransaction(t *testing.T) {
	scope := db.DSHScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code", Epoch: 7}
	store := &previewPublicationStore{}
	result, err := publishDSHBatch(t.Context(), store, scope, nil)
	if err != nil || result.Accepted != 0 || store.validations != 1 || store.publications != 0 || store.scope != scope {
		t.Fatalf("result=%+v err=%v store=%+v", result, err, store)
	}
}
func TestDurablePublicationKeepsTransactionPath(t *testing.T) {
	store := &previewPublicationStore{}
	result, err := publishDSHBatch(t.Context(), store, db.DSHScope{Epoch: 7}, []db.DSHPublicEvent{{ID: "message"}})
	if err != nil || result.Accepted != 1 || store.validations != 0 || store.publications != 1 {
		t.Fatalf("result=%+v err=%v store=%+v", result, err, store)
	}
}

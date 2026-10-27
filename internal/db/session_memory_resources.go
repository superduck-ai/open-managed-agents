package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

func enforceSessionMemoryResourceInvariantsTx(
	ctx context.Context,
	executor yourbatis.Executor,
	resource SessionResource,
) error {
	if resource.ResourceType != SessionResourceTypeMemoryStore {
		return nil
	}
	if resource.MemoryStore == nil || resource.MemoryStore.ExternalID == "" {
		return ErrPreconditionFailed
	}
	storeID := resource.MemoryStore.ExternalID
	mapper := NewSessionResourceMapper(executor)
	duplicates, err := mapper.CountSessionMemoryStoresByStoreID(
		ctx,
		resource.WorkspaceUUID,
		resource.SessionExternalID,
		storeID,
	)
	if err != nil {
		return err
	}
	if duplicates > 0 {
		return &SessionMemoryStoreDuplicateError{}
	}
	active, err := mapper.CountSessionFileResources(
		ctx,
		resource.WorkspaceUUID,
		resource.SessionExternalID,
		SessionResourceTypeMemoryStore,
	)
	if err != nil {
		return err
	}
	if active+1 > MaxSessionMemoryStores {
		return &SessionMemoryStoreLimitError{Limit: MaxSessionMemoryStores}
	}
	return nil
}

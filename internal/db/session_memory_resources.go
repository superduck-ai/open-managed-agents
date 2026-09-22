package db

import (
	"context"
	"encoding/json"

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
	storeID, err := memoryStoreIDFromPayload(resource.Payload)
	if err != nil {
		return err
	}
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

func memoryStoreIDFromPayload(payload json.RawMessage) (string, error) {
	var body struct {
		MemoryStoreID string `json:"memory_store_id"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || body.MemoryStoreID == "" {
		return "", ErrPreconditionFailed
	}
	return body.MemoryStoreID, nil
}

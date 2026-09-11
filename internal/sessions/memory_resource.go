package sessions

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func (h *Handler) memoryStorePayload(
	ctx context.Context,
	session db.Session,
	body *sessionResourceRequest,
	attachSet *sessionresource.MemoryAttachSet,
	resourceID string,
) (map[string]any, error) {
	if err := sessionresource.RejectClientMemoryIdentityFields(body.MountPath, body.Name, body.Description); err != nil {
		return nil, err
	}
	memoryStoreID, err := parseRequiredRawString(body.MemoryStoreID, "memory_store_id")
	if err != nil {
		return nil, err
	}
	store, err := h.db.GetMemoryStore(ctx, session.WorkspaceUUID, memoryStoreID)
	if err != nil {
		return nil, resourceReferenceError{
			ResourceType: sessionresource.MemoryStoreType,
			ResourceID:   memoryStoreID,
			Err:          err,
		}
	}
	if store.ArchivedAt != nil {
		return nil, resourceReferenceError{
			ResourceType: sessionresource.MemoryStoreType,
			ResourceID:   memoryStoreID,
			Err:          db.ErrInvalidState,
		}
	}
	access, err := sessionresource.ParseMemoryAccess(body.Access)
	if err != nil {
		return nil, err
	}
	instructions, err := sessionresource.ParseMemoryInstructions(body.Instructions)
	if err != nil {
		return nil, err
	}
	slug, err := attachSet.Add(memoryStoreID, store.Name, store.ExternalID)
	if err != nil {
		return nil, err
	}
	snapshot := sessionresource.SnapshotMemoryStore(
		memoryStoreID,
		access,
		instructions,
		store.Name,
		store.Description,
		slug,
	)
	return snapshot.PayloadFields(resourceID), nil
}

func observeSessionMemoryResources(resources []db.SessionResource) *sessionresource.MemoryAttachSet {
	attachSet := sessionresource.NewMemoryAttachSet()
	for _, resource := range resources {
		sessionresource.ObserveStoredMemoryResource(attachSet, resource.ResourceType, resource.Payload)
	}
	return attachSet
}

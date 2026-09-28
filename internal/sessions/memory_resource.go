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
) (sessionresource.MemorySnapshotPayload, error) {
	spec, store, err := sessionresource.ResolveMemoryAttach(ctx, h.db, session.WorkspaceUUID, sessionresource.MemoryAttachRequest{
		MemoryStoreID: body.MemoryStoreID, Access: body.Access, Instructions: body.Instructions,
		MountPath: body.MountPath, Name: body.Name, Description: body.Description,
	})
	if err != nil {
		return sessionresource.MemorySnapshotPayload{}, err
	}
	snapshot, err := spec.Snapshot(store, attachSet)
	if err != nil {
		return sessionresource.MemorySnapshotPayload{}, err
	}
	return snapshot.Payload(resourceID), nil
}

func observeSessionMemoryResources(resources []db.SessionResource) *sessionresource.MemoryAttachSet {
	attachSet := sessionresource.NewMemoryAttachSet()
	for _, resource := range resources {
		sessionresource.ObserveStoredMemoryResource(attachSet, resource.ResourceType, resource.Payload)
	}
	return attachSet
}

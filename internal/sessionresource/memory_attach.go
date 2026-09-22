package sessionresource

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// MemoryAttachSpec is the persisted template, without server-authored identity.
type MemoryAttachSpec struct {
	MemoryStoreID string  `json:"memory_store_id,omitempty"`
	Access        string  `json:"access,omitempty"`
	Instructions  *string `json:"instructions,omitempty"`
}

// MemoryAttachRequest retains field presence at the API boundary.
type MemoryAttachRequest struct {
	MemoryStoreID json.RawMessage
	Access        json.RawMessage
	Instructions  json.RawMessage
	MountPath     json.RawMessage
	Name          json.RawMessage
	Description   json.RawMessage
}

type MemoryStoreReader interface {
	GetMemoryStore(context.Context, string, string) (db.MemoryStore, error)
}

func ParseMemoryAttach(request MemoryAttachRequest) (MemoryAttachSpec, error) {
	if err := RejectClientMemoryIdentityFields(request.MountPath, request.Name, request.Description); err != nil {
		return MemoryAttachSpec{}, err
	}
	id, err := requiredString(request.MemoryStoreID, "memory_store_id")
	if err != nil {
		return MemoryAttachSpec{}, err
	}
	access, err := ParseMemoryAccess(request.Access)
	if err != nil {
		return MemoryAttachSpec{}, err
	}
	spec := MemoryAttachSpec{MemoryStoreID: id, Access: access}
	if len(request.Instructions) > 0 {
		instructions, err := ParseMemoryInstructions(request.Instructions)
		if err != nil {
			return MemoryAttachSpec{}, err
		}
		spec.Instructions = &instructions
	}
	return spec, nil
}

func ResolveMemoryAttach(ctx context.Context, reader MemoryStoreReader, workspaceUUID string, request MemoryAttachRequest) (MemoryAttachSpec, db.MemoryStore, error) {
	spec, err := ParseMemoryAttach(request)
	if err != nil {
		return spec, db.MemoryStore{}, err
	}
	store, err := LoadMemoryStore(ctx, reader, workspaceUUID, spec.MemoryStoreID)
	return spec, store, err
}

func LoadMemoryStore(ctx context.Context, reader MemoryStoreReader, workspaceUUID, id string) (db.MemoryStore, error) {
	store, err := reader.GetMemoryStore(ctx, workspaceUUID, id)
	if err == nil && store.ArchivedAt != nil {
		err = db.ErrInvalidState
	}
	if err != nil {
		return db.MemoryStore{}, ReferenceError{ResourceType: MemoryStoreType, ResourceID: id, Err: err}
	}
	return store, nil
}

func ParseStoredMemoryAttach(raw json.RawMessage) (MemoryAttachSpec, error) {
	var fields struct {
		MemoryStoreID json.RawMessage `json:"memory_store_id"`
		Access        json.RawMessage `json:"access"`
		Instructions  json.RawMessage `json:"instructions"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return MemoryAttachSpec{}, err
	}
	// Historical templates allow an empty access value as the default.
	var access string
	if len(fields.Access) > 0 && !isJSONNull(fields.Access) {
		if err := json.Unmarshal(fields.Access, &access); err != nil {
			return MemoryAttachSpec{}, ErrMemoryStoreAccess
		}
	}
	if access == "" {
		fields.Access = nil
	}
	return ParseMemoryAttach(MemoryAttachRequest{MemoryStoreID: fields.MemoryStoreID, Access: fields.Access, Instructions: fields.Instructions})
}

func (s MemoryAttachSpec) Snapshot(store db.MemoryStore, set *MemoryAttachSet) (MemorySnapshot, error) {
	slug, err := set.Add(s.MemoryStoreID, store.Name, store.ExternalID)
	if err != nil {
		return MemorySnapshot{}, err
	}
	instructions := ""
	if s.Instructions != nil {
		instructions = *s.Instructions
	}
	return SnapshotMemoryStore(s.MemoryStoreID, s.Access, instructions, store.Name, store.Description, slug), nil
}

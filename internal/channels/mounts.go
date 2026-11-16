package channels

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type channelMountInput struct {
	MemoryStoreID *string `json:"memory_store_id"`
	VaultID       *string `json:"vault_id"`
}

func (h *handler) validateMounts(w http.ResponseWriter, r *http.Request, workspaceUUID string) error {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		return invalidChannelUpdate(err)
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	if len(data) == 0 {
		return nil
	}
	var mounts channelMountInput
	if err := json.Unmarshal(data, &mounts); err != nil {
		return invalidChannelUpdate(err)
	}
	if mounts.MemoryStoreID != nil {
		store, err := h.store.GetMemoryStore(r.Context(), workspaceUUID, *mounts.MemoryStoreID)
		if errors.Is(err, db.ErrNotFound) || (err == nil && (store.ArchivedAt != nil || store.DeletedAt != nil)) {
			return invalidMemoryMount(err)
		}
		if err != nil {
			return unavailable(err)
		}
	}
	if mounts.VaultID != nil {
		vault, err := h.store.GetVault(r.Context(), workspaceUUID, *mounts.VaultID)
		if errors.Is(err, db.ErrNotFound) || (err == nil && (vault.ArchivedAt != nil || vault.DeletedAt != nil)) {
			return invalidVaultMount(err)
		}
		if err != nil {
			return unavailable(err)
		}
	}
	return nil
}

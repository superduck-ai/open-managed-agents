package codesessions

import (
	"context"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type sandboxToolRequest struct {
	Method string `json:"method"`
	Params struct {
		Name      string `json:"name"`
		Arguments struct {
			FilePath string  `json:"file_path"`
			Content  *string `json:"content"`
		} `json:"arguments"`
	} `json:"params"`
}

type sandboxMemoryErrorText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (h *Handler) loadSandboxMemoryMounts(ctx context.Context, session db.CodeSession) ([]db.SessionMemoryMount, error) {
	filesystem, err := h.db.GetFilestoreFilesystemBySession(ctx, session.WorkspaceUUID, session.SessionExternalID)
	if err != nil {
		return nil, err
	}
	return h.db.ListSessionMemoryMounts(ctx, session.WorkspaceUUID, filesystem.UUID)
}

func memoryMutationDenial(mounts []db.SessionMemoryMount, target string) string {
	var selected *db.SessionMemoryMount
	for i := range mounts {
		mount := &mounts[i]
		if mount.MountPath == "" || (target != mount.MountPath && !strings.HasPrefix(target, mount.MountPath+"/")) {
			continue
		}
		if selected != nil {
			return "Memory mount identity is ambiguous"
		}
		selected = mount
	}
	if selected == nil || selected.StoreMissing || selected.MemoryStoreUUID == "" {
		return "Memory path is not attached to an available store"
	}
	if selected.Archived {
		return "Memory store is archived and read-only"
	}
	if selected.Access != string(sessionresource.MemoryAccessReadWrite) {
		return "Memory mount is read-only"
	}
	return ""
}

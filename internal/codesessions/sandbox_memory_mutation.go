package codesessions

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type sandboxRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  struct {
		Name string `json:"name"`
		Meta struct {
			ProtocolVersion string `json:"io.modelcontextprotocol/protocolVersion"`
		} `json:"_meta"`
		Arguments struct {
			FilePath string `json:"file_path"`
		} `json:"arguments"`
	} `json:"params"`
}

type sandboxMemoryToolResult struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  struct {
		ResultType string                   `json:"resultType,omitempty"`
		IsError    bool                     `json:"isError"`
		Content    []sandboxMemoryErrorText `json:"content"`
	} `json:"result"`
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

func (h *Handler) rejectMemoryToolMutation(w http.ResponseWriter, r *http.Request, session db.CodeSession, call sandboxRPCRequest) bool {
	name := strings.ToLower(call.Params.Name)
	if call.Method != "tools/call" || (name != "write" && name != "edit") || session.SessionExternalID == "" {
		return false
	}
	target := path.Clean(call.Params.Arguments.FilePath)
	if target != sessionresource.MemoryMountRoot && !strings.HasPrefix(target, sessionresource.MemoryMountRoot+"/") {
		return false
	}
	mounts, err := h.loadMemoryToolMounts(r.Context(), session)
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return true
	}
	reason := memoryMutationDenial(mounts, target)
	if reason == "" {
		return false
	}
	response := sandboxMemoryToolResult{JSONRPC: "2.0", ID: call.ID}
	if call.Params.Meta.ProtocolVersion == "2026-07-28" || r.Header.Get("Mcp-Protocol-Version") == "2026-07-28" {
		response.Result.ResultType = "complete"
	}
	response.Result.IsError = true
	response.Result.Content = []sandboxMemoryErrorText{{Type: "text", Text: reason}}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
	return true
}

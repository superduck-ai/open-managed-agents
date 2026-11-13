package codesessions

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type memoryToolCallState struct {
	BeforeID       string `json:"before_id"`
	ExpectedSHA256 string `json:"expected_sha256"`
	SessionID      string `json:"session_id"`
	StoreID        string `json:"store_id"`
	MemoryPath     string `json:"memory_path"`
}

func (h *Handler) prepareToolCallMemory(r *http.Request, session db.CodeSession, request toolCallSubmission) ([]byte, *string, []byte, error) {
	var call sandboxToolRequest
	call.Method, call.Params.Name = "tools/call", request.Tool
	if err := json.Unmarshal(request.Input, &call.Params.Arguments); err != nil {
		return nil, nil, nil, err
	}
	if call.Method == "tools/call" && (call.Params.Name == "write" || call.Params.Name == "edit") {
		call.Params.Arguments.FilePath = path.Clean(call.Params.Arguments.FilePath)
		if !isMemoryToolMutation(call) && call.Params.Arguments.FilePath != sessionresource.MemoryMountRoot {
			return nil, nil, nil, nil
		}
		mounts, err := h.loadMemoryToolMounts(r.Context(), session)
		if err != nil {
			return nil, nil, nil, err
		}
		if reason := memoryMutationDenial(mounts, call.Params.Arguments.FilePath); reason != "" {
			failure, err := json.Marshal(struct {
				IsError bool                     `json:"isError"`
				Content []sandboxMemoryErrorText `json:"content"`
			}{true, []sandboxMemoryErrorText{{Type: "text", Text: reason}}})
			return failure, nil, nil, err
		}
	}
	checkpoint, err := h.prepareMemoryToolCallState(r.Context(), session, call)
	if err != nil || checkpoint == nil {
		return nil, nil, nil, err
	}
	state, err := json.Marshal(checkpoint)
	name := "memory_version"
	return nil, &name, state, err
}

func (h *Handler) finalizeToolCall(ctx context.Context, call db.SandboxToolCall, wait bool) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	var state memoryToolCallState
	var err error
	if call.Finalizer == nil || *call.Finalizer != "memory_version" || json.Unmarshal(call.FinalizerState, &state) != nil {
		err = errSandboxUnavailable
	} else {
		session := db.CodeSession{OrganizationUUID: call.OrganizationUUID, WorkspaceUUID: call.WorkspaceUUID, SessionExternalID: state.SessionID}
		commit := memoryToolCommit{BeforeID: state.BeforeID, ExpectedSHA256: state.ExpectedSHA256, SessionID: state.SessionID, Load: func(ctx context.Context) (memoryToolVersion, error) {
			return h.loadMemoryToolVersion(ctx, session, state.StoreID, state.MemoryPath)
		}}
		if wait {
			err = commit.wait(ctx)
		} else {
			version, loadErr := commit.Load(ctx)
			err = loadErr
			if err == nil && (version.ID == "" || version.ID == state.BeforeID || version.SessionID != state.SessionID || (state.ExpectedSHA256 != "" && version.SHA256 != state.ExpectedSHA256)) {
				err = errSandboxUnavailable
			}
		}
	}
	var failure []byte
	if err != nil {
		failure = []byte(`{"isError":true,"content":[{"type":"text","text":"Memory mutation was executed, but its immutable version commit could not be confirmed. Read authoritative Memory state before retrying."}]}`)
	}
	result, err := h.db.FinalizeSandboxToolCall(ctx, call, failure)
	if err != nil {
		h.logger.ErrorContext(ctx, "tool call finalization failed", "call_uuid", call.UUID, "error", err)
		return
	}
	h.notifyToolCall(result)
}

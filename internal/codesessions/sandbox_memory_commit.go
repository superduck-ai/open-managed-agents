package codesessions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type memoryToolVersion struct {
	ID        string
	SHA256    string
	SessionID string
}

type memoryToolCommit struct {
	BeforeID       string
	ExpectedSHA256 string
	SessionID      string
	Load           func(context.Context) (memoryToolVersion, error)
}

func (c *memoryToolCommit) wait(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		version, err := c.Load(ctx)
		if err != nil {
			return err
		}
		if version.ID != "" && version.ID != c.BeforeID && version.SessionID == c.SessionID && (c.ExpectedSHA256 == "" || version.SHA256 == c.ExpectedSHA256) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func isMemoryToolMutation(call sandboxRPCRequest) bool {
	name := strings.ToLower(call.Params.Name)
	target := path.Clean(call.Params.Arguments.FilePath)
	return call.Method == "tools/call" && (name == "write" || name == "edit") && strings.HasPrefix(target, sessionresource.MemoryMountRoot+"/")
}

func (h *Handler) prepareMemoryToolCommit(ctx context.Context, session db.CodeSession, call sandboxRPCRequest) (*memoryToolCommit, error) {
	if !isMemoryToolMutation(call) || session.SessionExternalID == "" {
		return nil, nil
	}
	mounts, err := h.loadMemoryToolMounts(ctx, session)
	if err != nil {
		return nil, err
	}
	target := path.Clean(call.Params.Arguments.FilePath)
	if reason := memoryMutationDenial(mounts, target); reason != "" {
		return nil, fmt.Errorf("prepare Memory commit: %s", reason)
	}
	for _, mount := range mounts {
		if !strings.HasPrefix(target, mount.MountPath+"/") {
			continue
		}
		storeID, memoryPath := mount.MemoryStoreExternalID, strings.TrimPrefix(target, mount.MountPath)
		load := func(ctx context.Context) (memoryToolVersion, error) {
			return h.loadMemoryToolVersion(ctx, session, storeID, memoryPath)
		}
		before, err := load(ctx)
		if err != nil {
			return nil, err
		}
		commit := &memoryToolCommit{BeforeID: before.ID, SessionID: session.SessionExternalID, Load: load}
		if strings.EqualFold(call.Params.Name, "write") && call.Params.Arguments.Content != nil {
			digest := sha256.Sum256([]byte(*call.Params.Arguments.Content))
			commit.ExpectedSHA256 = hex.EncodeToString(digest[:])
		}
		return commit, nil
	}
	return nil, fmt.Errorf("Memory mutation target has no file path")
}

func (h *Handler) loadSandboxMemoryVersion(ctx context.Context, session db.CodeSession, storeID, memoryPath string) (memoryToolVersion, error) {
	memory, found, err := h.db.GetMemoryByPath(ctx, session.WorkspaceUUID, storeID, memoryPath)
	if err != nil || !found {
		return memoryToolVersion{}, err
	}
	version, err := h.db.GetMemoryVersion(ctx, session.WorkspaceUUID, storeID, memory.CurrentVersionExternalID)
	if err != nil {
		return memoryToolVersion{}, err
	}
	result := memoryToolVersion{ID: version.ExternalID, SHA256: memory.ContentSHA256}
	if version.CreatedBy.Type == db.MemoryActorTypeSession {
		result.SessionID = version.CreatedBy.SessionID
	}
	return result, nil
}

type sandboxToolCompletion struct {
	ID     json.RawMessage `json:"id"`
	Result *struct {
		IsError    bool   `json:"isError"`
		ResultType string `json:"resultType"`
	} `json:"result"`
}

func (c *memoryToolCommit) confirm(ctx context.Context, call sandboxRPCRequest, payload []byte) ([]byte, error) {
	if c == nil {
		return payload, nil
	}
	var result sandboxToolCompletion
	if json.Unmarshal(payload, &result) != nil || !bytes.Equal(bytes.TrimSpace(result.ID), bytes.TrimSpace(call.ID)) || result.Result == nil || result.Result.IsError || (result.Result.ResultType != "" && result.Result.ResultType != "complete") {
		return payload, nil
	}
	if err := c.wait(ctx); err != nil {
		failure := sandboxMemoryToolResult{JSONRPC: "2.0", ID: call.ID}
		failure.Result.ResultType = result.Result.ResultType
		failure.Result.IsError = true
		failure.Result.Content = []sandboxMemoryErrorText{{Type: "text", Text: "Memory mutation was executed, but its immutable version commit could not be confirmed. Read the authoritative Memory state before retrying."}}
		body, marshalErr := json.Marshal(failure)
		if marshalErr != nil {
			return nil, marshalErr
		}
		return body, err
	}
	return payload, nil
}

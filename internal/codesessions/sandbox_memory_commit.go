package codesessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func isMemoryToolMutation(call sandboxToolRequest) bool {
	name := strings.ToLower(call.Params.Name)
	target := path.Clean(call.Params.Arguments.FilePath)
	return call.Method == "tools/call" && (name == "write" || name == "edit") && strings.HasPrefix(target, sessionresource.MemoryMountRoot+"/")
}

func (h *Handler) prepareMemoryToolCallState(ctx context.Context, session db.CodeSession, call sandboxToolRequest) (*memoryToolCallState, error) {
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
		state := &memoryToolCallState{StoreID: mount.MemoryStoreExternalID, MemoryPath: strings.TrimPrefix(target, mount.MountPath), SessionID: session.SessionExternalID}
		before, err := h.loadMemoryToolVersion(ctx, session, state.StoreID, state.MemoryPath)
		if err != nil {
			return nil, err
		}
		state.BeforeID = before.ID
		if strings.EqualFold(call.Params.Name, "write") && call.Params.Arguments.Content != nil {
			digest := sha256.Sum256([]byte(*call.Params.Arguments.Content))
			state.ExpectedSHA256 = hex.EncodeToString(digest[:])
		}
		return state, nil
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

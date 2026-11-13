package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSandboxMemoryMutationAuthority(t *testing.T) {
	active := db.SessionMemoryMount{MemoryStoreUUID: "store", MemoryStoreExternalID: "memstore_owned", MountPath: "/mnt/memory/owned", Access: "read_write"}
	archived := active
	archived.Archived = true
	readonly := active
	readonly.Access = "read_only"
	missing := active
	missing.StoreMissing = true
	for _, tc := range []struct {
		name   string
		mounts []db.SessionMemoryMount
		target string
		want   string
	}{
		{"archived", []db.SessionMemoryMount{archived}, "/mnt/memory/owned/file", "archived"},
		{"readonly", []db.SessionMemoryMount{readonly}, "/mnt/memory/owned/file", "read-only"},
		{"missing", []db.SessionMemoryMount{missing}, "/mnt/memory/owned/file", "available"},
		{"ambiguous", []db.SessionMemoryMount{active, archived}, "/mnt/memory/owned/file", "ambiguous"},
		{"unattached", []db.SessionMemoryMount{active}, "/mnt/memory/other/file", "attached"},
		{"prefix collision", []db.SessionMemoryMount{active}, "/mnt/memory/owned-other/file", "attached"},
		{"traversal", []db.SessionMemoryMount{active}, "/mnt/memory/owned/../other/file", "attached"},
		{"active", []db.SessionMemoryMount{active}, "/mnt/memory/owned/file", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := db.CodeSession{WorkspaceUUID: "workspace", SessionExternalID: "session"}
			h := &Handler{loadMemoryToolMounts: func(_ context.Context, got db.CodeSession) ([]db.SessionMemoryMount, error) {
				if got.WorkspaceUUID != session.WorkspaceUUID || got.SessionExternalID != session.SessionExternalID {
					t.Fatal("lost authenticated scope")
				}
				return tc.mounts, nil
			}, loadMemoryToolVersion: func(context.Context, db.CodeSession, string, string) (memoryToolVersion, error) {
				return memoryToolVersion{ID: "before"}, nil
			}}
			input, _ := json.Marshal(struct {
				FilePath string `json:"file_path"`
				Content  string `json:"content"`
			}{tc.target, "new"})
			failure, finalizer, state, err := h.prepareToolCallMemory(httptest.NewRequest("POST", "/tool-calls", nil), session, toolCallSubmission{Tool: "write", Input: input})
			if err != nil {
				t.Fatal(err)
			}
			if tc.want != "" {
				if !strings.Contains(string(failure), tc.want) || finalizer != nil {
					t.Fatalf("invalid refusal: %s %v", failure, finalizer)
				}
			} else if failure != nil || finalizer == nil || len(state) == 0 {
				t.Fatalf("missing finalizer: %s %v %s", failure, finalizer, state)
			}
		})
	}
}

func TestSandboxMemoryMutationLookupFailureStopsDispatch(t *testing.T) {
	failure := errors.New("storage lookup failed")
	h := &Handler{loadMemoryToolMounts: func(context.Context, db.CodeSession) ([]db.SessionMemoryMount, error) { return nil, failure }}
	_, _, _, err := h.prepareToolCallMemory(httptest.NewRequest("POST", "/tool-calls", nil), db.CodeSession{SessionExternalID: "owned"}, toolCallSubmission{Tool: "write", Input: []byte(`{"file_path":"/mnt/memory/owned/file"}`)})
	if !errors.Is(err, failure) {
		t.Fatalf("lookup failure swallowed: %v", err)
	}
}

package codesessions

import (
	"context"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
	"time"
)

func TestMemoryToolCommitRejectsUncommittedSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version memoryToolVersion
	}{
		{"same version", memoryToolVersion{ID: "before", SHA256: "wanted", SessionID: "owned"}},
		{"wrong content", memoryToolVersion{ID: "after", SHA256: "other", SessionID: "owned"}},
		{"other session", memoryToolVersion{ID: "after", SHA256: "wanted", SessionID: "foreign"}},
		{"API actor", memoryToolVersion{ID: "after", SHA256: "wanted"}},
		{"not uploaded", memoryToolVersion{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
			defer cancel()
			reads := 0
			c := memoryToolCommit{BeforeID: "before", ExpectedSHA256: "wanted", SessionID: "owned", Load: func(context.Context) (memoryToolVersion, error) { reads++; return tc.version, nil }}
			if err := c.wait(ctx); !errors.Is(err, context.DeadlineExceeded) || reads == 0 {
				t.Fatalf("acknowledged uncommitted snapshot: reads=%d err=%v", reads, err)
			}
		})
	}
}

func TestMemoryToolCommitWaitsForOwnExactSnapshot(t *testing.T) {
	reads := 0
	c := memoryToolCommit{BeforeID: "before", ExpectedSHA256: "wanted", SessionID: "owned", Load: func(context.Context) (memoryToolVersion, error) {
		reads++
		if reads == 1 {
			return memoryToolVersion{ID: "before", SHA256: "wanted", SessionID: "owned"}, nil
		}
		return memoryToolVersion{ID: "after", SHA256: "wanted", SessionID: "owned"}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.wait(ctx); err != nil || reads < 2 {
		t.Fatalf("did not wait for immutable commit: reads=%d err=%v", reads, err)
	}
}

func TestMemoryToolCommitPropagatesStorageFailure(t *testing.T) {
	failure := errors.New("storage unavailable")
	c := memoryToolCommit{Load: func(context.Context) (memoryToolVersion, error) { return memoryToolVersion{}, failure }}
	if !errors.Is(c.wait(context.Background()), failure) {
		t.Fatal("masked authoritative storage failure")
	}
}

func memoryCommitTestCall(name, target string) sandboxToolRequest {
	call := sandboxToolRequest{Method: "tools/call"}
	call.Params.Name = name
	call.Params.Arguments.FilePath = target
	return call
}

func TestMemoryToolCommitPreparationUsesAuthenticatedMount(t *testing.T) {
	session := db.CodeSession{WorkspaceUUID: "owned-workspace", SessionExternalID: "owned-session"}
	loads := 0
	h := &Handler{
		loadMemoryToolMounts: func(context.Context, db.CodeSession) ([]db.SessionMemoryMount, error) {
			return []db.SessionMemoryMount{{MemoryStoreUUID: "store-uuid", MemoryStoreExternalID: "store-id", MountPath: "/mnt/memory/project", Access: "read_write"}}, nil
		},
		loadMemoryToolVersion: func(_ context.Context, got db.CodeSession, storeID, memoryPath string) (memoryToolVersion, error) {
			loads++
			if (got.WorkspaceUUID != session.WorkspaceUUID || got.SessionExternalID != session.SessionExternalID) || storeID != "store-id" || memoryPath != "/nested/note" {
				t.Fatalf("lost authenticated mount: %+v %s %s", got, storeID, memoryPath)
			}
			return memoryToolVersion{ID: "seed"}, nil
		},
	}
	call := memoryCommitTestCall("Write", "/mnt/memory/project/nested/note")
	content := "abc"
	call.Params.Arguments.Content = &content
	commit, err := h.prepareMemoryToolCallState(context.Background(), session, call)
	if err != nil || commit == nil || commit.BeforeID != "seed" || commit.SessionID != "owned-session" || commit.ExpectedSHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" || commit.StoreID != "store-id" || commit.MemoryPath != "/nested/note" {
		t.Fatalf("invalid checkpoint %+v %v", commit, err)
	}
	for _, other := range []sandboxToolRequest{memoryCommitTestCall("Read", call.Params.Arguments.FilePath), memoryCommitTestCall("Write", "/tmp/file"), memoryCommitTestCall("Write", "/mnt/memory-other/file"), memoryCommitTestCall("Bash", call.Params.Arguments.FilePath)} {
		if commit, err := h.prepareMemoryToolCallState(context.Background(), session, other); err != nil || commit != nil {
			t.Fatal("changed unrelated operation")
		}
	}
	if loads != 1 {
		t.Fatal("loaded versions for unrelated operations")
	}
}

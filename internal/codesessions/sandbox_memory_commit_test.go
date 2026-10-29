package codesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"net/http/httptest"
	"strings"
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

func memoryCommitTestCall(name, target string) sandboxRPCRequest {
	call := sandboxRPCRequest{JSONRPC: "2.0", ID: json.RawMessage(`7`), Method: "tools/call"}
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
	commit, err := h.prepareMemoryToolCommit(context.Background(), session, call)
	if err != nil || commit == nil || commit.BeforeID != "seed" || commit.SessionID != "owned-session" || commit.ExpectedSHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("invalid checkpoint %+v %v", commit, err)
	}
	for _, other := range []sandboxRPCRequest{memoryCommitTestCall("Read", call.Params.Arguments.FilePath), memoryCommitTestCall("Write", "/tmp/file"), memoryCommitTestCall("Write", "/mnt/memory-other/file"), memoryCommitTestCall("Bash", call.Params.Arguments.FilePath)} {
		if commit, err := h.prepareMemoryToolCommit(context.Background(), session, other); err != nil || commit != nil {
			t.Fatal("changed unrelated operation")
		}
	}
	if loads != 1 {
		t.Fatal("loaded versions for unrelated operations")
	}
}

func TestMemoryToolCommitConfirmsOnlySuccessfulMatchingResult(t *testing.T) {
	calls := 0
	commit := &memoryToolCommit{BeforeID: "seed", SessionID: "owned", Load: func(context.Context) (memoryToolVersion, error) {
		calls++
		return memoryToolVersion{ID: "new", SessionID: "owned"}, nil
	}}
	call := memoryCommitTestCall("Edit", "/mnt/memory/project/file")
	for _, payload := range []string{`{"jsonrpc":"2.0","id":8,"result":{}}`, `{"jsonrpc":"2.0","id":7,"error":{"code":-1}}`, `{"jsonrpc":"2.0","id":7,"result":{"isError":true}}`, `{"jsonrpc":"2.0","method":"notifications/progress"}`, `{"jsonrpc":"2.0","id":7,"result":{"resultType":"task"}}`} {
		got, err := commit.confirm(context.Background(), call, []byte(payload))
		if err != nil || string(got) != payload || calls != 0 {
			t.Fatal("waited for a failed, unrelated, or incomplete result")
		}
	}
	payload := []byte(`{"jsonrpc":"2.0","id":7,"result":{"resultType":"complete","content":[{"type":"text","text":"done"}]}}`)
	got, err := commit.confirm(context.Background(), call, payload)
	if err != nil || !bytes.Equal(got, payload) || calls != 1 {
		t.Fatal("changed successful response or failed to confirm")
	}
}

func TestMemoryToolCommitFailureCannotReportSuccess(t *testing.T) {
	commit := &memoryToolCommit{Load: func(context.Context) (memoryToolVersion, error) {
		return memoryToolVersion{}, errors.New("internal-secret-must-not-leak")
	}}
	call := memoryCommitTestCall("Write", "/mnt/memory/project/file")
	payload := []byte(`{"jsonrpc":"2.0","id":7,"result":{"resultType":"complete","content":[]}}`)
	got, err := commit.confirm(context.Background(), call, payload)
	var result sandboxMemoryToolResult
	if err == nil || json.Unmarshal(got, &result) != nil || !result.Result.IsError || result.Result.ResultType != "complete" || string(result.ID) != "7" || bytes.Contains(got, []byte("internal-secret")) {
		t.Fatalf("unsafe completion: %s %v", got, err)
	}
}

func TestMemoryToolStreamPreservesEventsAndWaitsBeforeCompletion(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		writer := httptest.NewRecorder()
		progress := strings.ReplaceAll("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n", "\n", newline)
		complete := strings.ReplaceAll("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"content\":[]}}\n\n", "\n", newline)
		calls := 0
		commit := &memoryToolCommit{BeforeID: "seed", SessionID: "owned", Load: func(context.Context) (memoryToolVersion, error) {
			calls++
			if writer.Body.String() != progress {
				t.Fatalf("completion forwarded before commit: %q", writer.Body.String())
			}
			return memoryToolVersion{ID: "new", SessionID: "owned"}, nil
		}}
		if confirmErr, err := streamMemoryToolResponse(context.Background(), writer, strings.NewReader(progress+complete), memoryCommitTestCall("Edit", "/mnt/memory/project/file"), commit); confirmErr != nil || err != nil || calls != 1 || writer.Body.String() != progress+complete {
			t.Fatalf("stream changed: %q calls%d err%v", writer.Body.String(), calls, err)
		}
	}
}

func TestMemoryToolStreamSeparatesConfirmationFailure(t *testing.T) {
	writer := httptest.NewRecorder()
	commit := &memoryToolCommit{Load: func(context.Context) (memoryToolVersion, error) {
		return memoryToolVersion{}, errors.New("memory store race")
	}}
	complete := "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"content\":[]}}\n\n"
	confirmErr, err := streamMemoryToolResponse(context.Background(), writer, strings.NewReader(complete), memoryCommitTestCall("Write", "/mnt/memory/project/file"), commit)
	if confirmErr == nil || err != nil {
		t.Fatalf("confirmErr=%v err=%v", confirmErr, err)
	}
}

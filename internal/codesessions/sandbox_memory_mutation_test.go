package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func TestSandboxMemoryMutationAuthority(t *testing.T) {
	session := db.CodeSession{WorkspaceUUID: "owned-workspace", SessionExternalID: "sesn_owned"}
	active := db.SessionMemoryMount{MemoryStoreUUID: "owned-store", MemoryStoreExternalID: "memstore_owned", MountPath: "/mnt/memory/owned", Access: "read_write"}
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
		{"archived former RW", []db.SessionMemoryMount{archived}, "/mnt/memory/owned/note.txt", "archived"},
		{"read only", []db.SessionMemoryMount{readonly}, "/mnt/memory/owned/note.txt", "read-only"},
		{"missing live store", []db.SessionMemoryMount{missing}, "/mnt/memory/owned/note.txt", "available"},
		{"ambiguous mount", []db.SessionMemoryMount{active, archived}, "/mnt/memory/owned/note.txt", "ambiguous"},
		{"unattached parent", []db.SessionMemoryMount{active}, "/mnt/memory/unattached/note.txt", "attached"},
		{"mount prefix collision", []db.SessionMemoryMount{active}, "/mnt/memory/owned-other/note.txt", "attached"},
		{"canonical traversal to parent", []db.SessionMemoryMount{active}, "/mnt/memory/owned/../other/note.txt", "attached"},
		{"active RW preserved", []db.SessionMemoryMount{active}, "/mnt/memory/owned/nested/note.txt", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"write", "Edit"} {
				h := &Handler{loadMemoryToolMounts: func(_ context.Context, got db.CodeSession) ([]db.SessionMemoryMount, error) {
					if got.WorkspaceUUID != session.WorkspaceUUID || got.SessionExternalID != session.SessionExternalID {
						t.Fatal("lost authenticated scope")
					}
					return tc.mounts, nil
				}}
				call := sandboxRPCRequest{JSONRPC: "2.0", Method: "tools/call", ID: json.RawMessage(`"owned_rpc"`)}
				call.Params.Name = name
				call.Params.Arguments.FilePath = tc.target
				w := httptest.NewRecorder()
				handled := h.rejectMemoryToolMutation(w, httptest.NewRequest(http.MethodPost, "/sandbox/mcp", nil), session, call)
				if handled != (tc.want != "") {
					t.Fatalf("handled=%v want reason%q", handled, tc.want)
				}
				if !handled {
					continue
				}
				var response sandboxMemoryToolResult
				if json.Unmarshal(w.Body.Bytes(), &response) != nil || string(response.ID) != `"owned_rpc"` || !response.Result.IsError || w.Code != 200 || len(response.Result.Content) != 1 || !strings.Contains(response.Result.Content[0].Text, tc.want) {
					t.Fatalf("invalid actual MCP refusal: status%d body%s", w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestSandboxMemoryMutationReloadsArchiveState(t *testing.T) {
	archived := false
	loads := 0
	h := &Handler{loadMemoryToolMounts: func(context.Context, db.CodeSession) ([]db.SessionMemoryMount, error) {
		loads++
		return []db.SessionMemoryMount{{MemoryStoreUUID: "store", MountPath: "/mnt/memory/owned", Access: "read_write", Archived: archived}}, nil
	}}
	call := sandboxRPCRequest{JSONRPC: "2.0", Method: "tools/call", ID: json.RawMessage(`9`)}
	call.Params.Name = "write"
	call.Params.Arguments.FilePath = "/mnt/memory/owned/file"
	session := db.CodeSession{SessionExternalID: "sesn_owned"}
	request := httptest.NewRequest(http.MethodPost, "/sandbox/mcp", nil)
	if h.rejectMemoryToolMutation(httptest.NewRecorder(), request, session, call) {
		t.Fatal("active mount blocked")
	}
	archived = true
	if !h.rejectMemoryToolMutation(httptest.NewRecorder(), request, session, call) || loads != 2 {
		t.Fatal("archive state cached across calls")
	}
}

func TestSandboxMemoryMutationPreservesOtherToolsAndNamespaces(t *testing.T) {
	loads := 0
	h := &Handler{loadMemoryToolMounts: func(context.Context, db.CodeSession) ([]db.SessionMemoryMount, error) { loads++; return nil, nil }}
	for _, tc := range []struct{ method, name, path string }{
		{"tools/call", "read", "/mnt/memory/owned/file"}, {"tools/call", "bash", "/mnt/memory/owned/file"},
		{"initialize", "write", "/mnt/memory/owned/file"}, {"tools/call", "write", "/mnt/memory-other/file"},
		{"tools/call", "write", "/tmp/owned.txt"},
	} {
		call := sandboxRPCRequest{Method: tc.method}
		call.Params.Name = tc.name
		call.Params.Arguments.FilePath = tc.path
		if h.rejectMemoryToolMutation(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/sandbox/mcp", nil), db.CodeSession{SessionExternalID: "sesn_owned"}, call) {
			t.Fatalf("unrelated tool blocked: %+v", tc)
		}
	}
	if loads != 0 {
		t.Fatal("unrelated calls queried Memory policy")
	}
}

func TestSandboxMemoryMutationLookupFailureStopsDispatch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &Handler{errorAdapter: httpapi.NewErrorAdapter(logger), loadMemoryToolMounts: func(context.Context, db.CodeSession) ([]db.SessionMemoryMount, error) {
		return nil, errors.New("secret storage detail")
	}}
	call := sandboxRPCRequest{Method: "tools/call"}
	call.Params.Name = "write"
	call.Params.Arguments.FilePath = "/mnt/memory/owned/file"
	w := httptest.NewRecorder()
	if !h.rejectMemoryToolMutation(w, httptest.NewRequest(http.MethodPost, "/sandbox/mcp", nil), db.CodeSession{SessionExternalID: "sesn_owned"}, call) || w.Code != 500 || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("lookup failure did not fail closed safely: %d %s", w.Code, w.Body.String())
	}
}

func TestSandboxMemoryMutationModernResultEnvelope(t *testing.T) {
	h := &Handler{loadMemoryToolMounts: func(context.Context, db.CodeSession) ([]db.SessionMemoryMount, error) {
		return []db.SessionMemoryMount{{MemoryStoreUUID: "owned", MountPath: "/mnt/memory/owned", Access: "read_write", Archived: true}}, nil
	}}
	for _, version := range []string{"2026-07-28", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			call := sandboxRPCRequest{JSONRPC: "2.0", Method: "tools/call", ID: json.RawMessage(`7`)}
			call.Params.Name = "write"
			call.Params.Arguments.FilePath = "/mnt/memory/owned/note.txt"
			call.Params.Meta.ProtocolVersion = version
			request := httptest.NewRequest(http.MethodPost, "/sandbox/mcp", nil)
			request.Header.Set("Mcp-Protocol-Version", version)
			w := httptest.NewRecorder()
			if !h.rejectMemoryToolMutation(w, request, db.CodeSession{SessionExternalID: "sesn_owned"}, call) {
				t.Fatal("archive not rejected")
			}
			var response sandboxMemoryToolResult
			if json.Unmarshal(w.Body.Bytes(), &response) != nil || !response.Result.IsError {
				t.Fatalf("invalid result: %s", w.Body.String())
			}
			if (response.Result.ResultType == "complete") != (version == "2026-07-28") {
				t.Fatalf("wrong version envelope: %s", w.Body.String())
			}
			if version == "2026-07-28" {
				t.Logf("DENIAL_PROBE %s", w.Body.String())
			}
		})
	}
}

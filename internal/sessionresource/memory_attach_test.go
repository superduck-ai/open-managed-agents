package sessionresource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type memoryReaderStub struct {
	store         db.MemoryStore
	err           error
	calls         int
	workspace, id string
}

func (r *memoryReaderStub) GetMemoryStore(_ context.Context, workspace, id string) (db.MemoryStore, error) {
	r.calls++
	r.workspace = workspace
	r.id = id
	return r.store, r.err
}

func TestResolveMemoryAttach(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name             string
		request          MemoryAttachRequest
		store            db.MemoryStore
		loadErr, wantErr error
		calls            int
	}{
		{name: "client identity", request: MemoryAttachRequest{MountPath: json.RawMessage(`null`)}, wantErr: ErrMemoryStoreClientIdentity},
		{name: "invalid access", request: MemoryAttachRequest{MemoryStoreID: json.RawMessage(`"mem_1"`), Access: json.RawMessage(`"write"`)}, wantErr: ErrMemoryStoreAccess},
		{name: "instructions too long", request: MemoryAttachRequest{MemoryStoreID: json.RawMessage(`"mem_1"`), Instructions: json.RawMessage(`"` + strings.Repeat("字", 501) + `"`)}, wantErr: ErrMemoryStoreInstructionsTooLong},
		{name: "missing", request: MemoryAttachRequest{MemoryStoreID: json.RawMessage(`"mem_1"`)}, loadErr: db.ErrNotFound, wantErr: db.ErrNotFound, calls: 1},
		{name: "archived", request: MemoryAttachRequest{MemoryStoreID: json.RawMessage(`"mem_1"`)}, store: db.MemoryStore{ArchivedAt: &now}, wantErr: db.ErrInvalidState, calls: 1},
		{name: "default", request: MemoryAttachRequest{MemoryStoreID: json.RawMessage(`"mem_1"`)}, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &memoryReaderStub{store: test.store, err: test.loadErr}
			spec, _, err := ResolveMemoryAttach(t.Context(), reader, "workspace_1", test.request)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if reader.calls != test.calls {
				t.Fatalf("calls = %d", reader.calls)
			}
			if test.calls > 0 && (reader.workspace != "workspace_1" || reader.id != "mem_1") {
				t.Fatalf("incorrect scope: %+v", reader)
			}
			if err == nil && (spec.Access != MemoryAccessReadWrite || spec.Instructions != nil) {
				t.Fatalf("incorrect defaults: %+v", spec)
			}
		})
	}
}

func TestMemoryAttachInstructionsPresence(t *testing.T) {
	for _, raw := range []string{"", `null`, `""`, `"  keep whitespace  "`} {
		t.Run(raw, func(t *testing.T) {
			spec, err := ParseMemoryAttach(MemoryAttachRequest{MemoryStoreID: json.RawMessage(`"mem_1"`), Instructions: json.RawMessage(raw)})
			if err != nil {
				t.Fatal(err)
			}
			if (spec.Instructions == nil) != (raw == "") {
				t.Fatalf("presence lost: %+v", spec)
			}
			encoded, err := json.Marshal(spec)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := ParseStoredMemoryAttach(encoded)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := stored.Snapshot(db.MemoryStore{Name: "Project", ExternalID: "mem_1"}, NewMemoryAttachSet())
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if raw == `"  keep whitespace  "` {
				want = "  keep whitespace  "
			}
			if snapshot.Instructions != want || snapshot.MountPath != "/mnt/memory/project" {
				t.Fatalf("snapshot = %+v", snapshot)
			}
		})
	}
}

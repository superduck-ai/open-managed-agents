package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type memoryLoadReader struct {
	calls    map[string]int
	err      error
	archived bool
}

func (r *memoryLoadReader) GetMemoryStore(_ context.Context, workspace, id string) (db.MemoryStore, error) {
	if workspace != "workspace_1" {
		return db.MemoryStore{}, errors.New("wrong workspace")
	}
	r.calls[id]++
	store := db.MemoryStore{ExternalID: id, Name: "Current name"}
	if r.archived {
		now := time.Now()
		store.ArchivedAt = &now
	}
	return store, r.err
}

func TestDeploymentMemoryLoadsOncePerStore(t *testing.T) {
	for _, test := range []struct {
		name     string
		err      error
		archived bool
		want     error
	}{
		{name: "missing", err: db.ErrNotFound, want: db.ErrNotFound},
		{name: "archived", archived: true, want: db.ErrInvalidState},
		{name: "unavailable", err: context.DeadlineExceeded, want: context.DeadlineExceeded},
		{name: "available"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &memoryLoadReader{calls: map[string]int{}, err: test.err, archived: test.archived}
			resources := json.RawMessage(`[{"type":"memory_store","memory_store_id":"mem_1"},{"type":"memory_store","memory_store_id":"mem_1"},{"type":"file","file_id":"file_1"}]`)
			stores, err := loadDeploymentMemoryStores(t.Context(), reader, "workspace_1", resources)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if reader.calls["mem_1"] != 1 || len(reader.calls) != 1 {
				t.Fatalf("calls = %v", reader.calls)
			}
			if err == nil && stores["mem_1"].Name != "Current name" {
				t.Fatalf("stores = %v", stores)
			}
		})
	}
}

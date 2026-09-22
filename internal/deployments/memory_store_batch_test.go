package deployments

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type deploymentResourceReaderStub struct {
	stores     []db.MemoryStore
	fileErr    error
	loadErr    error
	loadedIDs  []string
	batchCalls int
	fileCalls  int
}

func (s *deploymentResourceReaderStub) GetFile(context.Context, string, string) (db.FileRecord, error) {
	s.fileCalls++
	return db.FileRecord{}, s.fileErr
}

func (s *deploymentResourceReaderStub) GetMemoryStoresByExternalIDs(_ context.Context, _ string, ids []string) ([]db.MemoryStore, error) {
	s.batchCalls++
	s.loadedIDs = ids
	return s.stores, s.loadErr
}

func TestValidateDeploymentResources(t *testing.T) {
	now := time.Now().UTC()
	memory := func(id string) deploymentResourcePayload {
		return deploymentResourcePayload{Type: "memory_store", MemoryStoreID: id}
	}
	file := deploymentResourcePayload{Type: "file", FileID: "file_1"}
	active := db.MemoryStore{ExternalID: "active", Name: "snapshot name"}
	archived := db.MemoryStore{ExternalID: "archived", ArchivedAt: &now}
	unavailable := errors.New("database unavailable")
	tests := []struct {
		name          string
		resources     []deploymentResourcePayload
		fileErr       error
		loadErr       error
		wantFailure   string
		wantErr       error
		wantIDs       []string
		wantFileCalls int
	}{
		{name: "missing before archived", resources: []deploymentResourcePayload{memory("missing"), memory("archived")}, wantFailure: "session_resource_not_found_error", wantIDs: []string{"missing", "archived"}},
		{name: "archived before missing", resources: []deploymentResourcePayload{memory("archived"), memory("missing")}, wantFailure: "memory_store_archived_error", wantIDs: []string{"archived", "missing"}},
		{name: "file failure before memory prevents loading", resources: []deploymentResourcePayload{file, memory("archived")}, fileErr: db.ErrNotFound, wantFailure: "file_not_found_error", wantFileCalls: 1},
		{name: "file between stores wins over later archived", resources: []deploymentResourcePayload{memory("active"), file, memory("archived")}, fileErr: db.ErrNotFound, wantFailure: "file_not_found_error", wantIDs: []string{"active", "archived"}, wantFileCalls: 1},
		{name: "archived memory before file wins", resources: []deploymentResourcePayload{memory("archived"), file}, fileErr: db.ErrNotFound, wantFailure: "memory_store_archived_error", wantIDs: []string{"archived"}},
		{name: "database failure propagates", resources: []deploymentResourcePayload{memory("active")}, loadErr: unavailable, wantErr: unavailable, wantIDs: []string{"active"}},
		{name: "empty resources skip loading"},
		{name: "files only skip memory loading", resources: []deploymentResourcePayload{file}, wantFileCalls: 1},
		{name: "reuse one batch with repeated ids", resources: []deploymentResourcePayload{memory("active"), file, memory("active")}, wantIDs: []string{"active"}, wantFileCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &deploymentResourceReaderStub{stores: []db.MemoryStore{archived, active}, fileErr: tt.fileErr, loadErr: tt.loadErr}
			stores, failure, err := validateDeploymentResources(context.Background(), reader, "workspace", tt.resources)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			failureType := ""
			if failure != nil {
				failureType = failure.Type
			}
			if failureType != tt.wantFailure {
				t.Fatalf("failure = %q, want %q", failureType, tt.wantFailure)
			}
			wantBatchCalls := 0
			if len(tt.wantIDs) > 0 {
				wantBatchCalls = 1
			}
			if reader.batchCalls != wantBatchCalls || reader.fileCalls != tt.wantFileCalls || !reflect.DeepEqual(reader.loadedIDs, tt.wantIDs) {
				t.Fatalf("calls: batch=%d ids=%v file=%d, want batch=%d ids=%v file=%d", reader.batchCalls, reader.loadedIDs, reader.fileCalls, wantBatchCalls, tt.wantIDs, tt.wantFileCalls)
			}
			if failure == nil && err == nil && len(tt.wantIDs) > 0 && stores["active"].Name != active.Name {
				t.Fatalf("validated stores did not retain snapshot data: %v", stores)
			}
		})
	}
}

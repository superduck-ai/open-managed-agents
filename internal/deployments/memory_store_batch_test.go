package deployments

import (
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDeploymentMemoryStoresByID(t *testing.T) {
	now := time.Now().UTC()
	archived := db.MemoryStore{ExternalID: "archived", ArchivedAt: &now}
	active := db.MemoryStore{ExternalID: "active"}
	tests := []struct {
		name    string
		ids     []string
		rows    []db.MemoryStore
		wantErr error
	}{
		{name: "missing first", ids: []string{"missing", "archived"}, rows: []db.MemoryStore{archived}, wantErr: db.ErrNotFound},
		{name: "archived first", ids: []string{"archived", "missing"}, rows: []db.MemoryStore{archived}, wantErr: db.ErrInvalidState},
		{name: "all missing", ids: []string{"missing"}, wantErr: db.ErrNotFound},
		{name: "unordered results", ids: []string{"active", "other"}, rows: []db.MemoryStore{{ExternalID: "other"}, active}},
		{name: "repeated ID", ids: []string{"active", "active"}, rows: []db.MemoryStore{active}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores, err := deploymentMemoryStoresByID(tt.ids, tt.rows)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if stores != nil {
					t.Fatalf("stores = %v, want nil on failure", stores)
				}
				return
			}
			for _, id := range tt.ids {
				if stores[id].ExternalID != id {
					t.Fatalf("missing store %q in %v", id, stores)
				}
			}
		})
	}
}

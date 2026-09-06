package deployments

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestStoreRejectsWritesBeforeConfigure(t *testing.T) {
	store := NewStore(nil)
	if _, err := store.Create(t.Context(), db.Deployment{}); !errors.Is(err, errStoreNotConfigured) {
		t.Fatalf("Create() error = %v, want unconfigured store", err)
	}
	if err := store.Configure(t.Context(), nil); !errors.Is(err, errStoreNotConfigured) {
		t.Fatalf("Configure(nil) error = %v, want unconfigured store", err)
	}
}

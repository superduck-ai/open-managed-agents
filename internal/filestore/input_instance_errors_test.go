package filestore

import (
	"errors"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestMountedSourceMutationError(t *testing.T) {
	result := mapDatabaseError("remove source", db.ErrFileInUse)
	if result.Status != http.StatusConflict || result.Code != "failed_precondition" || !errors.Is(result, db.ErrFileInUse) {
		t.Fatalf("mounted source error=%+v", result)
	}
}

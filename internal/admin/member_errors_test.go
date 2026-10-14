package admin

import (
	"errors"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestLastOrganizationAdminConflict(t *testing.T) {
	err := mapAdminDBError(db.ErrLastOrganizationAdmin, "User not found")
	result, ok := errors.AsType[*serviceError](err)
	if !ok || result.status != http.StatusConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
}

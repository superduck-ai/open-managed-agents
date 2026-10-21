package admin

import (
	"errors"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestReportLoadErrorMapsOnlyInvalidState(t *testing.T) {
	invalid := reportLoadError(db.ErrInvalidState)
	var serviceErr *serviceError
	if !errors.As(invalid, &serviceErr) || serviceErr.status != http.StatusBadRequest {
		t.Fatalf("invalid state = %#v", invalid)
	}
	databaseErr := errors.New("database unavailable")
	if !errors.Is(reportLoadError(databaseErr), databaseErr) {
		t.Fatal("database error was rewritten")
	}
}

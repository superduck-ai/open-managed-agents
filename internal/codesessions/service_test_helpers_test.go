package codesessions

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func newTestService(t *testing.T, database *db.DB) *Service {
	t.Helper()
	credentials, err := NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatalf("create code session credentials: %v", err)
	}
	return NewServiceWithCredentials(database, credentials)
}

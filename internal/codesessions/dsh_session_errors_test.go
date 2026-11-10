package codesessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDSHUnavailableSessionPreservesScopeAndState(t *testing.T) {
	failure := errors.New("lookup failed")
	now := time.Now().UTC()
	for _, test := range []struct {
		name      string
		session   db.Session
		found     bool
		err, want error
	}{
		{"other tenant", db.Session{OrganizationUUID: "other", Status: "terminated"}, true, nil, db.ErrNotFound},
		{"absent", db.Session{}, false, nil, db.ErrNotFound},
		{"lookup failure", db.Session{}, false, failure, failure},
		{"terminated", db.Session{OrganizationUUID: "org", Status: "terminated"}, true, nil, db.ErrDSHSessionTerminated},
		{"archived", db.Session{OrganizationUUID: "org", ArchivedAt: &now}, true, nil, db.ErrDSHSessionArchived},
		{"active session with unavailable code", db.Session{OrganizationUUID: "org", Status: "idle"}, true, nil, db.ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := SessionCredentialClaims{OrganizationUUID: "org", WorkspaceUUID: "workspace", PublicSessionID: "session"}
			find := func(_ context.Context, workspace, id string) (db.Session, bool, error) {
				if workspace != claims.WorkspaceUUID || id != claims.PublicSessionID {
					t.Fatalf("unscoped lookup: %s %s", workspace, id)
				}
				return test.session, test.found, test.err
			}
			if err := dshUnavailableSessionError(t.Context(), find, claims); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

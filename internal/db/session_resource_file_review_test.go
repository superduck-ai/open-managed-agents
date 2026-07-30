package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestSessionNamespaceInsertErrorMapsMissingSession(t *testing.T) {
	if err := mapSessionNamespaceInsertError(sql.ErrNoRows); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mapSessionNamespaceInsertError(sql.ErrNoRows) = %v, want ErrNotFound", err)
	}
}

func TestMoveFilestoreFileResultQueryCarriesNamespaceScope(t *testing.T) {
	for _, predicate := range []string{
		"workspace_uuid = :workspace_uuid",
		"session_uuid = :session_uuid",
		"id = :entry_id",
	} {
		if !strings.Contains(moveFilestoreFileResultQuery, predicate) {
			t.Fatalf("move result query lacks %q: %s", predicate, moveFilestoreFileResultQuery)
		}
	}
}

func TestFilestoreFilesystemRowRejectsMissingSession(t *testing.T) {
	if _, err := (filestoreFilesystemRow{}).filesystem(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("filestoreFilesystemRow.filesystem() error = %v, want ErrNotFound", err)
	}
}

func TestSessionResourceFileSourceSQLReadsStableScopeWithoutOwnershipJoins(t *testing.T) {
	query := sessionResourceFileSourceSQL()
	for _, required := range []string{
		"resource.organization_uuid",
		"resource.workspace_uuid",
		"resource.session_uuid",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("namespace source query lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"join organizations",
		"join workspaces",
		"join sessions",
		"join filestore_filesystems",
		"resource.organization_id",
		"resource.workspace_id",
		"resource.session_id",
	} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("namespace source query retains %q", forbidden)
		}
	}
}

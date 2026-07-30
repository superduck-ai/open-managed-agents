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
		"filesystem_uuid = :filesystem_uuid",
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

func TestSessionNamespaceNodeSourceSQLFiltersDeletedFilesystems(t *testing.T) {
	const predicate = "filesystem.deleted_at is null"
	if !strings.Contains(sessionNamespaceNodeSourceSQL(), predicate) {
		t.Fatalf("namespace source query lacks %q", predicate)
	}
}

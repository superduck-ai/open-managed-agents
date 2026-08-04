package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionNamespaceInsertErrorMapsMissingSession(t *testing.T) {
	if err := mapSessionNamespaceInsertError(sql.ErrNoRows); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mapSessionNamespaceInsertError(sql.ErrNoRows) = %v, want ErrNotFound", err)
	}
}

func TestMoveFilestoreFileResultQueryCarriesNamespaceScope(t *testing.T) {
	bound := buildSessionResourceFileMapperGetResourceFileForMoveResult(
		yourbatis.DialectPostgres,
		sessionResourceIdentityParams{},
	)
	for _, predicate := range []string{
		"workspace_uuid = $1",
		"session_uuid = $2",
		"uuid = $3",
	} {
		if !strings.Contains(bound.SQL, predicate) {
			t.Fatalf("move result query lacks %q: %s", predicate, bound.SQL)
		}
	}
}

func TestFilestoreFilesystemRowRejectsMissingSession(t *testing.T) {
	if _, err := (filestoreFilesystemRow{}).filesystem(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("filestoreFilesystemRow.filesystem() error = %v, want ErrNotFound", err)
	}
}

func TestSessionResourceFileMapperReadsStableScopeWithoutOwnershipJoins(t *testing.T) {
	query := buildFilestoreCleanupMapperListFilesystemFiles(
		yourbatis.DialectPostgres,
		filestoreFilesystemBatchMapperParams{},
	).SQL
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

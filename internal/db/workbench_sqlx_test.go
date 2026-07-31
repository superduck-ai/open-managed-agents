package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/google/uuid"
)

func TestWorkbenchRevisionQueryBindsNamedArguments(t *testing.T) {
	query, arguments, err := bindNamed(postgresRebinder{}, upsertWorkbenchRevisionQuery, map[string]any{
		"organization_uuid": uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		"prompt_uuid":       "prompt_test",
		"revision_uuid":     "revision_test",
		"payload":           `{"model":"test"}`,
	})
	if err != nil {
		t.Fatalf("bind named query: %v", err)
	}
	if strings.Contains(query, ":") {
		t.Fatalf("query retains named parameter syntax: %q", query)
	}
	if strings.Contains(query, "::") {
		t.Fatalf("query contains PostgreSQL shorthand cast: %q", query)
	}
	if strings.Contains(query, " AS uuid)") {
		t.Fatalf("query contains UUID cast ceremony: %q", query)
	}
	if len(arguments) != 6 {
		t.Fatalf("argument count = %d, want 6", len(arguments))
	}
}

func TestWorkbenchSQLXRowsMapDatabaseValues(t *testing.T) {
	prompt := workbenchPromptRow{
		OrgUUID:            uuid.MustParse("00000000-0000-4000-8000-000000000002"),
		PromptUUID:         "prompt_test",
		WorkspaceUUID:      uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		WorkspaceDisplayID: "workspace_test",
		LatestRevisionUUID: sql.NullString{String: "revision_test", Valid: true},
	}.record()
	if prompt.LatestRevisionUUID == nil || *prompt.LatestRevisionUUID != "revision_test" {
		t.Fatalf("latest revision = %#v, want revision_test", prompt.LatestRevisionUUID)
	}

	evaluation := workbenchEvaluationRow{PayloadJSON: `{"score":1}`}.record()
	if evaluation.Payload["score"] != float64(1) {
		t.Fatalf("evaluation payload = %#v, want score 1", evaluation.Payload)
	}
}

func TestWorkbenchNoRowsMapsToPlatformNotFound(t *testing.T) {
	if err := mapNoRows(sql.ErrNoRows); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("mapNoRows(sql.ErrNoRows) = %v, want platform.ErrNotFound", err)
	}
}

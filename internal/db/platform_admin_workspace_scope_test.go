package db

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestPlatformAdminWorkspaceScopeMapper(t *testing.T) {
	columns := []string{"organization_uuid"}
	t.Run("missing workspace", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: columns})
		if _, err := NewPlatformAdminOrganizationMapper(executor).FindWorkspaceOrganization(t.Context(), "wrkspc_missing"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing workspace error = %v", err)
		}
	})
	t.Run("execution failure", func(t *testing.T) {
		failure := errors.New("scope query failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: failure})
		if _, err := NewPlatformAdminOrganizationMapper(executor).FindWorkspaceOrganization(t.Context(), "wrkspc_test"); !errors.Is(err, failure) {
			t.Fatalf("execution error = %v", err)
		}
	})
	t.Run("invalid row", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: columns, rows: [][]driver.Value{{nil}}})
		if _, err := NewPlatformAdminOrganizationMapper(executor).FindWorkspaceOrganization(t.Context(), "wrkspc_test"); err == nil || errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("scan error = %v", err)
		}
	})
	t.Run("minimal scope projection", func(t *testing.T) {
		externalID := "wrkspc_'scope"
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: columns, rows: [][]driver.Value{{"organization-uuid"}}})
		row, err := NewPlatformAdminOrganizationMapper(executor).FindWorkspaceOrganization(t.Context(), externalID)
		if err != nil || row != "organization-uuid" {
			t.Fatalf("scope = %+v, error = %v", row, err)
		}
		assertMapperTestExecution(t, executor, "PlatformAdminOrganizationMapper.FindWorkspaceOrganization", yourbatis.StatementSelect, []any{externalID},
			"SELECT w.organization_uuid FROM workspaces w", "JOIN organizations o ON o.uuid = w.organization_uuid",
			"w.external_id = $1", "w.external_id <> 'default'", "w.archived_at IS NULL")
		if executor.bound.Args[0].Sensitive {
			t.Fatal("workspace external ID unexpectedly marked sensitive")
		}
	})
}

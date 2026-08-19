package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestMCPServerMapperPostgreSQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires project config: %v", err)
	}
	database, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("open project database: %v", err)
	}
	defer database.Close()

	tx, err := database.mapperDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatalf("begin mapper fixture transaction: %v", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("roll back mapper fixture transaction: %v", rollbackErr)
		}
	}()

	execMapperFixtureSQL(t, ctx, tx, `
		CREATE TEMPORARY TABLE mcp_servers (
			id bigint generated always as identity primary key,
			uuid uuid NOT NULL UNIQUE,
			external_id text NOT NULL UNIQUE,
			organization_uuid uuid NOT NULL,
			workspace_uuid uuid NOT NULL,
			name text NOT NULL,
			transport_type text NOT NULL,
			endpoint_url text NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL,
			deleted_at timestamptz
		) ON COMMIT DROP;

		CREATE UNIQUE INDEX fixture_mcp_servers_workspace_name_unique
			ON mcp_servers (workspace_uuid, name)
			WHERE deleted_at IS NULL;

		CREATE UNIQUE INDEX fixture_mcp_servers_workspace_endpoint_unique
			ON mcp_servers (workspace_uuid, transport_type, endpoint_url)
			WHERE deleted_at IS NULL;
	`)
	mapper := NewMCPServerMapper(tx)
	now := time.Date(2026, time.August, 13, 1, 2, 3, 0, time.UTC)
	workspaceUUID := "00000000-0000-4000-8000-000000000002"
	otherWorkspaceUUID := "00000000-0000-4000-8000-000000000099"
	params := mcpServerWriteParams{
		UUID: "00000000-0000-4000-8000-000000000003", ExternalID: "mcpsrv_fixture",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: workspaceUUID,
		Name: "internal-docs", TransportType: "url", EndpointURL: "https://docs.example.test/mcp",
		CreatedAt: now, UpdatedAt: now,
	}

	t.Run("failure keeps workspace isolation and zero-row semantics", func(t *testing.T) {
		_, findErr := mapper.FindByExternalID(ctx, otherWorkspaceUUID, params.ExternalID)
		if !errors.Is(findErr, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() error = %v, want sql.ErrNoRows", findErr)
		}
		_, updateErr := mapper.UpdateByExternalID(ctx, mcpServerWriteParams{
			WorkspaceUUID: otherWorkspaceUUID, ExternalID: params.ExternalID,
			Name: "other", TransportType: "url", EndpointURL: "https://other.example.test/mcp", UpdatedAt: now,
		})
		if !errors.Is(updateErr, sql.ErrNoRows) {
			t.Fatalf("UpdateByExternalID() error = %v, want sql.ErrNoRows", updateErr)
		}
	})

	t.Run("success copies, lists, updates, and directly deletes reusable configurations", func(t *testing.T) {
		created, createErr := mapper.Insert(ctx, params)
		if createErr != nil || created.ExternalID != params.ExternalID || created.DeletedAt != nil {
			t.Fatalf("Insert() = (%+v, %v)", created, createErr)
		}

		updatedAt := now.Add(time.Minute)
		params.Name = "internal-docs-v2"
		params.EndpointURL = "https://docs.example.test/v2"
		params.UpdatedAt = updatedAt
		updated, updateErr := mapper.UpdateByExternalID(ctx, params)
		if updateErr != nil || updated.Name != params.Name || !updated.UpdatedAt.Equal(updatedAt) {
			t.Fatalf("UpdateByExternalID() = (%+v, %v)", updated, updateErr)
		}

		rows, listErr := mapper.ListPage(ctx, mcpServerPageMapperParams{
			WorkspaceUUID: workspaceUUID, Search: "DOCS-V2", FetchLimit: 2,
		})
		if listErr != nil || len(rows) != 1 || rows[0].UUID != params.UUID {
			t.Fatalf("ListPage() = (%+v, %v)", rows, listErr)
		}
		otherRows, listErr := mapper.ListPage(ctx, mcpServerPageMapperParams{
			WorkspaceUUID: otherWorkspaceUUID, FetchLimit: 2,
		})
		if listErr != nil || len(otherRows) != 0 {
			t.Fatalf("cross-workspace ListPage() = (%+v, %v)", otherRows, listErr)
		}

		deleted, deleteErr := mapper.SoftDeleteByExternalID(ctx, workspaceUUID, params.ExternalID)
		if deleteErr != nil || deleted.DeletedAt == nil {
			t.Fatalf("SoftDeleteByExternalID() = (%+v, %v)", deleted, deleteErr)
		}
		rows, listErr = mapper.ListPage(ctx, mcpServerPageMapperParams{WorkspaceUUID: workspaceUUID, FetchLimit: 2})
		if listErr != nil || len(rows) != 0 {
			t.Fatalf("ListPage() after delete = (%+v, %v)", rows, listErr)
		}
		_, findErr := mapper.FindByExternalID(ctx, workspaceUUID, params.ExternalID)
		if !errors.Is(findErr, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() after delete error = %v, want sql.ErrNoRows", findErr)
		}

		replacement := params
		replacement.UUID = "00000000-0000-4000-8000-000000000004"
		replacement.ExternalID = "mcp_replacement"
		replacement.CreatedAt = updatedAt.Add(time.Minute)
		replacement.UpdatedAt = replacement.CreatedAt
		createdReplacement, replacementErr := mapper.Insert(ctx, replacement)
		if replacementErr != nil || createdReplacement.Name != params.Name || createdReplacement.EndpointURL != params.EndpointURL {
			t.Fatalf("Insert() replacement after delete = (%+v, %v)", createdReplacement, replacementErr)
		}
	})
}

package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestAdminMCPTunnelMapperBuilderContracts(t *testing.T) {
	params := listAdminTunnelsMapperParams{
		OrganizationUUID:    "11111111-1111-4111-8111-111111111111",
		WorkspaceExternalID: "workspace_test",
		Limit:               21,
		Offset:              2,
	}
	tokenParams := updateAdminTunnelTokenParams{
		OrganizationUUID: params.OrganizationUUID,
		ExternalID:       "tunnel_test",
		TokenID:          "token_test",
		TunnelToken:      "secret-token",
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"find", mapperBuilderContract{
			statement: adminMCPTunnelMapperFindByExternalIDStatement,
			bound: buildAdminMCPTunnelMapperFindByExternalID(
				yourbatis.DialectPostgres, params.OrganizationUUID, "tunnel_test",
			),
			wantID:            "AdminMCPTunnelMapper.FindByExternalID",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "externalID"},
			wantSQLFragments:  []string{"FROM mcp_tunnels", "organization_uuid = $1", "external_id = $2"},
		}},
		{"list page", mapperBuilderContract{
			statement: adminMCPTunnelMapperListPageStatement,
			bound:     buildAdminMCPTunnelMapperListPage(yourbatis.DialectPostgres, params),
			wantID:    "AdminMCPTunnelMapper.ListPage",
			wantKind:  yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.OrganizationUUID", "params.WorkspaceExternalID", "params.Limit", "params.Offset",
			},
			wantSQLFragments: []string{
				"archived_at IS NULL", "workspace_external_id = $2",
				"ORDER BY created_at DESC, uuid DESC", "LIMIT $3", "OFFSET $4",
			},
		}},
		{"update token", mapperBuilderContract{
			statement: adminMCPTunnelMapperUpdateTokenByExternalIDStatement,
			bound:     buildAdminMCPTunnelMapperUpdateTokenByExternalID(yourbatis.DialectPostgres, tokenParams),
			wantID:    "AdminMCPTunnelMapper.UpdateTokenByExternalID",
			wantKind:  yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.TokenID", "params.TunnelToken", "params.OrganizationUUID", "params.ExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.TunnelToken"},
			wantSQLFragments:           []string{"UPDATE mcp_tunnels", "tunnel_token = $2", "archived_at IS NULL", "RETURNING"},
		}},
		{"archive", mapperBuilderContract{
			statement: adminMCPTunnelMapperArchiveByExternalIDStatement,
			bound: buildAdminMCPTunnelMapperArchiveByExternalID(
				yourbatis.DialectPostgres, params.OrganizationUUID, "tunnel_test",
			),
			wantID:            "AdminMCPTunnelMapper.ArchiveByExternalID",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"organizationUUID", "externalID"},
			wantSQLFragments:  []string{"COALESCE(archived_at, NOW())", "token_id = NULL", "RETURNING"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}

	t.Run("include archived without workspace", func(t *testing.T) {
		params.IncludeArchived = true
		params.WorkspaceExternalID = ""
		bound := buildAdminMCPTunnelMapperListPage(yourbatis.DialectPostgres, params)
		if strings.Contains(bound.SQL, "archived_at IS NULL") || strings.Contains(bound.SQL, "workspace_external_id =") {
			t.Fatalf("ListPage() retains optional filters: %q", bound.SQL)
		}
		if got := bound.Values(); len(got) != 3 {
			t.Fatalf("ListPage() argument count = %d, want 3", len(got))
		}
	})
}

func TestAdminMCPTunnelMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: adminMCPTunnelMapperTestColumns(),
		rows: [][]driver.Value{{
			"22222222-2222-4222-8222-222222222222",
			"tunnel_test",
			"11111111-1111-4111-8111-111111111111",
			"33333333-3333-4333-8333-333333333333",
			"workspace_test",
			"Test tunnel",
			"test.example.com",
			"token_test",
			"secret-token",
			createdAt,
			createdAt,
			nil,
		}},
	})
	row, err := NewAdminMCPTunnelMapper(executor).FindByExternalID(ctx, "org", "tunnel_test")
	tunnel, err := adminTunnelFromMapperRow(row, err)
	if err != nil || tunnel.UUID != row.UUID || tunnel.WorkspaceUUID == nil || *tunnel.WorkspaceUUID != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("FindByExternalID() = (%+v, %v)", tunnel, err)
	}
	if tunnel.TunnelToken == nil || *tunnel.TunnelToken != "secret-token" {
		t.Fatalf("FindByExternalID() token = %#v", tunnel.TunnelToken)
	}

	t.Run("zero rows", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminMCPTunnelMapperTestColumns()})
		row, err := NewAdminMCPTunnelMapper(executor).FindByExternalID(ctx, "org", "missing")
		_, mappedErr := adminTunnelFromMapperRow(row, err)
		if !errors.Is(err, sql.ErrNoRows) || !errors.Is(mappedErr, ErrNotFound) {
			t.Fatalf("FindByExternalID() errors = (%v, %v)", err, mappedErr)
		}
	})

}

func TestAdminMCPTunnelMapperPropagatesExecutionErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{"select", mapperExecutionErrorContract{
			statementID: "AdminMCPTunnelMapper.FindByExternalID",
			kind:        yourbatis.StatementSelect,
			query:       true,
			call: func(executor yourbatis.Executor) error {
				_, err := NewAdminMCPTunnelMapper(executor).FindByExternalID(ctx, "org", "tunnel")
				return err
			},
		}},
		{"returning", mapperExecutionErrorContract{
			statementID: "AdminMCPTunnelMapper.UpdateTokenByExternalID",
			kind:        yourbatis.StatementUpdate,
			query:       true,
			call: func(executor yourbatis.Executor) error {
				_, err := NewAdminMCPTunnelMapper(executor).UpdateTokenByExternalID(ctx, updateAdminTunnelTokenParams{})
				return err
			},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperExecutionError(t, test.contract) })
	}
}

func adminMCPTunnelMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "workspace_external_id",
		"display_name", "domain", "token_id", "tunnel_token", "created_at", "updated_at", "archived_at",
	}
}

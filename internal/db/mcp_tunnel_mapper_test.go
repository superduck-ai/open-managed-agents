package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestNextMCPTunnelTokenVersionRejectsStaleRotation(t *testing.T) {
	t.Parallel()
	if _, err := nextMCPTunnelTokenVersion(3, 2); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("nextMCPTunnelTokenVersion stale error = %v, want ErrInvalidState", err)
	}
}

func TestNextMCPTunnelTokenVersionAdvancesExpectedRotation(t *testing.T) {
	t.Parallel()
	version, err := nextMCPTunnelTokenVersion(3, 3)
	if err != nil || version != 4 {
		t.Fatalf("nextMCPTunnelTokenVersion = (%d, %v), want (4, nil)", version, err)
	}
}

func TestMCPTunnelMapperScopesEveryResourceLookup(t *testing.T) {
	t.Parallel()
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	workspaceUUID := "22222222-2222-4222-8222-222222222222"
	tunnelID := "tunnel_0123456789abcdef0123456789abcdef"
	tests := []mapperBuilderContract{
		{
			statement: mCPTunnelMapperFindByExternalIDStatement,
			bound: buildMCPTunnelMapperFindByExternalID(
				yourbatis.DialectPostgres, organizationUUID, workspaceUUID, tunnelID,
			),
			wantID: "MCPTunnelMapper.FindByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2", "external_id = $3"},
		},
		{
			statement: mCPTunnelMapperFindByDomainStatement,
			bound: buildMCPTunnelMapperFindByDomain(
				yourbatis.DialectPostgres, organizationUUID, workspaceUUID, "example.tunnel.invalid",
			),
			wantID: "MCPTunnelMapper.FindByDomain", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "domain"},
			wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2", "domain = $3"},
		},
		{
			statement: mCPTunnelMapperFindActiveForUpdateStatement,
			bound: buildMCPTunnelMapperFindActiveForUpdate(
				yourbatis.DialectPostgres, organizationUUID, workspaceUUID, tunnelID,
			),
			wantID: "MCPTunnelMapper.FindActiveForUpdate", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2", "external_id = $3", "archived_at IS NULL", "FOR UPDATE"},
		},
		{
			statement: mCPTunnelMapperArchiveByExternalIDStatement,
			bound: buildMCPTunnelMapperArchiveByExternalID(
				yourbatis.DialectPostgres, organizationUUID, workspaceUUID, tunnelID,
			),
			wantID: "MCPTunnelMapper.ArchiveByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"COALESCE(archived_at, NOW())", "organization_uuid = $1", "workspace_uuid = $2", "external_id = $3", "RETURNING"},
		},
	}
	for _, contract := range tests {
		assertMapperBuilderContract(t, contract)
	}
}

func TestMCPTunnelMapperListFiltersArchivedByDefault(t *testing.T) {
	t.Parallel()
	params := listMCPTunnelsMapperParams{
		OrganizationUUID: "organization", WorkspaceUUID: "workspace", Limit: 21, Offset: 2,
	}
	bound := buildMCPTunnelMapperListPage(yourbatis.DialectPostgres, params)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: mCPTunnelMapperListPageStatement, bound: bound,
		wantID: "MCPTunnelMapper.ListPage", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"params.OrganizationUUID", "params.WorkspaceUUID", "params.Limit", "params.Offset"},
		wantSQLFragments:  []string{"archived_at IS NULL", "ORDER BY created_at DESC, uuid DESC", "LIMIT $3", "OFFSET $4"},
	})
	params.IncludeArchived = true
	if sql := buildMCPTunnelMapperListPage(yourbatis.DialectPostgres, params).SQL; strings.Contains(sql, "archived_at IS NULL") {
		t.Fatalf("include-archived query still filters archived rows: %q", sql)
	}
}

func TestMCPTunnelTokenMapperProtectsCredentialArguments(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 20, 1, 0, 0, 0, time.UTC)
	insert := buildMCPTunnelTokenMapperInsert(yourbatis.DialectPostgres, insertMCPTunnelTokenParams{
		UUID: "token-uuid", ExternalID: "ttkn_example", TunnelUUID: "tunnel-uuid", Version: 1,
		TokenHash: []byte("hash"), Ciphertext: []byte("ciphertext"), Nonce: []byte("nonce"),
		WrappedDEK: []byte("wrapped"), FormatVersion: 1, KeyProvider: "local", KeyVersion: 1, CreatedAt: now,
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: mCPTunnelTokenMapperInsertStatement, bound: insert,
		wantID: "MCPTunnelTokenMapper.Insert", wantKind: yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.UUID", "params.ExternalID", "params.TunnelUUID", "params.Version",
			"params.TokenHash", "params.Ciphertext", "params.Nonce", "params.WrappedDEK",
			"params.FormatVersion", "params.KeyProvider", "params.KeyVersion", "params.CreatedAt",
		},
		wantSensitiveArgumentNames: []string{
			"params.TokenHash", "params.Ciphertext", "params.Nonce", "params.WrappedDEK",
		},
		wantSQLFragments: []string{"INSERT INTO mcp_tunnel_token_versions", "RETURNING"},
	})

	lookup := buildMCPTunnelTokenMapperFindByHashAndTunnelExternalID(
		yourbatis.DialectPostgres, []byte("hash"), "tunnel_0123456789abcdef0123456789abcdef",
	)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: mCPTunnelTokenMapperFindByHashAndTunnelExternalIDStatement, bound: lookup,
		wantID: "MCPTunnelTokenMapper.FindByHashAndTunnelExternalID", wantKind: yourbatis.StatementSelect,
		wantArgumentNames:          []string{"tokenHash", "tunnelExternalID"},
		wantSensitiveArgumentNames: []string{"tokenHash"},
		wantSQLFragments:           []string{"JOIN mcp_tunnels", "tv.token_hash = $1", "t.external_id = $2"},
	})
}

func TestMCPTunnelCertificateMapperScopesCRUDWithinTunnel(t *testing.T) {
	t.Parallel()
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	tunnelUUID := "22222222-2222-4222-8222-222222222222"
	certificateID := "tcrt_0123456789abcdef0123456789abcdef"
	now := time.Date(2026, time.August, 28, 1, 2, 3, 0, time.UTC)
	expiresAt := now.Add(24 * time.Hour)

	insert := buildMCPTunnelCertificateMapperInsert(yourbatis.DialectPostgres, insertMCPTunnelCertificateParams{
		UUID:             "33333333-3333-4333-8333-333333333333",
		ExternalID:       certificateID,
		OrganizationUUID: organizationUUID,
		TunnelUUID:       tunnelUUID,
		TunnelExternalID: "tunnel_0123456789abcdef0123456789abcdef",
		CACertificatePEM: "certificate",
		Fingerprint:      strings.Repeat("a", 64),
		ExpiresAt:        &expiresAt,
		CreatedAt:        now,
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: mCPTunnelCertificateMapperInsertStatement,
		bound:     insert,
		wantID:    "MCPTunnelCertificateMapper.Insert",
		wantKind:  yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.TunnelUUID",
			"params.TunnelExternalID", "params.CACertificatePEM", "params.Fingerprint",
			"params.ExpiresAt", "params.CreatedAt",
		},
		wantSQLFragments: []string{"INSERT INTO mcp_tunnel_certificates", "RETURNING"},
	})

	find := buildMCPTunnelCertificateMapperFindByExternalID(
		yourbatis.DialectPostgres,
		organizationUUID,
		tunnelUUID,
		certificateID,
	)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         mCPTunnelCertificateMapperFindByExternalIDStatement,
		bound:             find,
		wantID:            "MCPTunnelCertificateMapper.FindByExternalID",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID", "tunnelUUID", "externalID"},
		wantSQLFragments:  []string{"organization_uuid = $1", "tunnel_uuid = $2", "external_id = $3"},
	})

	archive := buildMCPTunnelCertificateMapperArchiveByExternalID(
		yourbatis.DialectPostgres,
		organizationUUID,
		tunnelUUID,
		certificateID,
	)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         mCPTunnelCertificateMapperArchiveByExternalIDStatement,
		bound:             archive,
		wantID:            "MCPTunnelCertificateMapper.ArchiveByExternalID",
		wantKind:          yourbatis.StatementUpdate,
		wantArgumentNames: []string{"organizationUUID", "tunnelUUID", "externalID"},
		wantSQLFragments: []string{
			"UPDATE mcp_tunnel_certificates",
			"COALESCE(archived_at, NOW())",
			"organization_uuid = $1",
			"tunnel_uuid = $2",
			"external_id = $3",
			"RETURNING",
		},
	})
}

func TestMCPTunnelCertificateMapperListFiltersArchivedByDefault(t *testing.T) {
	t.Parallel()
	params := listMCPTunnelCertificatesMapperParams{
		OrganizationUUID: "organization", TunnelUUID: "tunnel", Limit: 21, Offset: 2,
	}
	bound := buildMCPTunnelCertificateMapperListPage(yourbatis.DialectPostgres, params)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: mCPTunnelCertificateMapperListPageStatement, bound: bound,
		wantID: "MCPTunnelCertificateMapper.ListPage", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"params.OrganizationUUID", "params.TunnelUUID", "params.Limit", "params.Offset"},
		wantSQLFragments:  []string{"archived_at IS NULL", "ORDER BY created_at DESC, uuid DESC", "LIMIT $3", "OFFSET $4"},
	})
	params.IncludeArchived = true
	if sql := buildMCPTunnelCertificateMapperListPage(yourbatis.DialectPostgres, params).SQL; strings.Contains(sql, "archived_at IS NULL") {
		t.Fatalf("include-archived query still filters archived rows: %q", sql)
	}
}

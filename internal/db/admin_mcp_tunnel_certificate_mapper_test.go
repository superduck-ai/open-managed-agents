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

func TestAdminMCPTunnelCertificateMapperBuilderContracts(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	expiresAt := createdAt.Add(24 * time.Hour)
	insertParams := insertAdminTunnelCertificateParams{
		ExternalID:       "tcrt_test",
		OrganizationUUID: "11111111-1111-4111-8111-111111111111",
		TunnelUUID:       "22222222-2222-4222-8222-222222222222",
		TunnelExternalID: "tunnel_test",
		CACertificatePEM: "certificate-pem",
		Fingerprint:      "fingerprint",
		ExpiresAt:        &expiresAt,
		CreatedAt:        createdAt,
	}
	listParams := listAdminTunnelCertificatesMapperParams{
		OrganizationUUID: insertParams.OrganizationUUID,
		TunnelUUID:       insertParams.TunnelUUID,
		Limit:            21,
		Offset:           2,
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: adminMCPTunnelCertificateMapperInsertStatement,
			bound:     buildAdminMCPTunnelCertificateMapperInsert(yourbatis.DialectPostgres, insertParams),
			wantID:    "AdminMCPTunnelCertificateMapper.Insert",
			wantKind:  yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.ExternalID", "params.OrganizationUUID", "params.TunnelUUID",
				"params.TunnelExternalID", "params.CACertificatePEM", "params.Fingerprint",
				"params.ExpiresAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{"params.CACertificatePEM"},
			wantSQLFragments:           []string{"INSERT INTO mcp_tunnel_certificates", "VALUES", "RETURNING"},
		}},
		{"find", mapperBuilderContract{
			statement: adminMCPTunnelCertificateMapperFindByExternalIDStatement,
			bound: buildAdminMCPTunnelCertificateMapperFindByExternalID(
				yourbatis.DialectPostgres,
				insertParams.OrganizationUUID,
				insertParams.TunnelExternalID,
				insertParams.ExternalID,
			),
			wantID:            "AdminMCPTunnelCertificateMapper.FindByExternalID",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "tunnelExternalID", "certificateExternalID"},
			wantSQLFragments:  []string{"FROM mcp_tunnel_certificates", "organization_uuid = $1", "external_id = $3"},
		}},
		{"list page", mapperBuilderContract{
			statement: adminMCPTunnelCertificateMapperListPageStatement,
			bound:     buildAdminMCPTunnelCertificateMapperListPage(yourbatis.DialectPostgres, listParams),
			wantID:    "AdminMCPTunnelCertificateMapper.ListPage",
			wantKind:  yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.OrganizationUUID", "params.TunnelUUID", "params.Limit", "params.Offset",
			},
			wantSQLFragments: []string{
				"tunnel_uuid = $2", "archived_at IS NULL", "ORDER BY created_at DESC, uuid DESC", "LIMIT $3", "OFFSET $4",
			},
		}},
		{"archive", mapperBuilderContract{
			statement: adminMCPTunnelCertificateMapperArchiveByExternalIDStatement,
			bound: buildAdminMCPTunnelCertificateMapperArchiveByExternalID(
				yourbatis.DialectPostgres,
				insertParams.OrganizationUUID,
				insertParams.TunnelExternalID,
				insertParams.ExternalID,
			),
			wantID:            "AdminMCPTunnelCertificateMapper.ArchiveByExternalID",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"organizationUUID", "tunnelExternalID", "certificateExternalID"},
			wantSQLFragments:  []string{"COALESCE(archived_at, NOW())", "RETURNING"},
		}},
		{"count active", mapperBuilderContract{
			statement: adminMCPTunnelCertificateMapperCountActiveStatement,
			bound: buildAdminMCPTunnelCertificateMapperCountActive(
				yourbatis.DialectPostgres, insertParams.OrganizationUUID, insertParams.TunnelUUID,
			),
			wantID:            "AdminMCPTunnelCertificateMapper.CountActive",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "tunnelUUID"},
			wantSQLFragments:  []string{"CAST(COUNT(*) AS integer)", "archived_at IS NULL"},
		}},
		{"archive by tunnel", mapperBuilderContract{
			statement: adminMCPTunnelCertificateMapperArchiveActiveByTunnelUUIDStatement,
			bound: buildAdminMCPTunnelCertificateMapperArchiveActiveByTunnelUUID(
				yourbatis.DialectPostgres, insertParams.OrganizationUUID, insertParams.TunnelUUID,
			),
			wantID:            "AdminMCPTunnelCertificateMapper.ArchiveActiveByTunnelUUID",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"organizationUUID", "tunnelUUID"},
			wantSQLFragments:  []string{"UPDATE mcp_tunnel_certificates", "tunnel_uuid = $2", "archived_at IS NULL"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}

	t.Run("include archived", func(t *testing.T) {
		listParams.IncludeArchived = true
		bound := buildAdminMCPTunnelCertificateMapperListPage(yourbatis.DialectPostgres, listParams)
		if strings.Contains(bound.SQL, "archived_at IS NULL") {
			t.Fatalf("ListPage() unexpectedly filters archived certificates: %q", bound.SQL)
		}
	})
}

func TestAdminMCPTunnelCertificateMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: adminMCPTunnelCertificateMapperTestColumns(),
		rows: [][]driver.Value{{
			"33333333-3333-4333-8333-333333333333",
			"tcrt_test",
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
			"tunnel_test",
			"certificate-pem",
			"fingerprint",
			nil,
			createdAt,
			nil,
		}},
	})
	row, err := NewAdminMCPTunnelCertificateMapper(executor).Insert(ctx, insertAdminTunnelCertificateParams{})
	certificate, err := adminTunnelCertificateFromMapperRow(row, err)
	if err != nil || certificate.UUID != row.UUID || certificate.ExpiresAt != nil {
		t.Fatalf("Insert() = (%+v, %v)", certificate, err)
	}

	t.Run("scalar", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"count"}, rows: [][]driver.Value{{int64(2)}},
		})
		count, err := NewAdminMCPTunnelCertificateMapper(executor).CountActive(ctx, "org", "tunnel")
		if err != nil || count != 2 {
			t.Fatalf("CountActive() = (%d, %v), want 2, nil", count, err)
		}
	})

	t.Run("zero rows", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminMCPTunnelCertificateMapperTestColumns()})
		row, err := NewAdminMCPTunnelCertificateMapper(executor).FindByExternalID(ctx, "org", "tunnel", "missing")
		_, mappedErr := adminTunnelCertificateFromMapperRow(row, err)
		if !errors.Is(err, sql.ErrNoRows) || !errors.Is(mappedErr, ErrNotFound) {
			t.Fatalf("FindByExternalID() errors = (%v, %v)", err, mappedErr)
		}
	})

	t.Run("exec", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 2})
		err := NewAdminMCPTunnelCertificateMapper(executor).ArchiveActiveByTunnelUUID(ctx, "org", "tunnel")
		if err != nil || executor.execCallCount != 1 || executor.queryCallCount != 0 {
			t.Fatalf("ArchiveActiveByTunnelUUID() execution = (%d, %d, %v)", executor.queryCallCount, executor.execCallCount, err)
		}
	})

}

func TestAdminMCPTunnelCertificateMapperPropagatesExecutionErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{"select", mapperExecutionErrorContract{
			statementID: "AdminMCPTunnelCertificateMapper.FindByExternalID",
			kind:        yourbatis.StatementSelect,
			query:       true,
			call: func(executor yourbatis.Executor) error {
				_, err := NewAdminMCPTunnelCertificateMapper(executor).FindByExternalID(ctx, "org", "tunnel", "cert")
				return err
			},
		}},
		{"returning", mapperExecutionErrorContract{
			statementID: "AdminMCPTunnelCertificateMapper.Insert",
			kind:        yourbatis.StatementInsert,
			query:       true,
			call: func(executor yourbatis.Executor) error {
				_, err := NewAdminMCPTunnelCertificateMapper(executor).Insert(ctx, insertAdminTunnelCertificateParams{})
				return err
			},
		}},
		{"exec", mapperExecutionErrorContract{
			statementID: "AdminMCPTunnelCertificateMapper.ArchiveActiveByTunnelUUID",
			kind:        yourbatis.StatementUpdate,
			call: func(executor yourbatis.Executor) error {
				return NewAdminMCPTunnelCertificateMapper(executor).ArchiveActiveByTunnelUUID(ctx, "org", "tunnel")
			},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperExecutionError(t, test.contract) })
	}
}

func adminMCPTunnelCertificateMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "tunnel_uuid", "tunnel_external_id",
		"ca_certificate_pem", "fingerprint", "expires_at", "created_at", "archived_at",
	}
}

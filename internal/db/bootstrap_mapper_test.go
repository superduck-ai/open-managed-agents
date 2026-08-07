package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestConsoleOrganizationMapperBuilders(t *testing.T) {
	orgUUID := "11111111-1111-4111-8111-111111111111"
	name := "Console Organization"
	update := updateConsoleOrganizationParams{OrgUUID: orgUUID, Name: &name, Settings: []byte(`{"theme":"dark"}`)}
	profile := updateConsoleOrganizationProfileParams{OrgUUID: orgUUID, Profile: []byte(`{"company_name":"Console"}`)}

	contracts := []mapperBuilderContract{
		{
			statement:         consoleOrganizationMapperFindByUUIDStatement,
			bound:             buildConsoleOrganizationMapperFindByUUID(yourbatis.DialectPostgres, orgUUID),
			wantID:            "ConsoleOrganizationMapper.FindByUUID",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"orgUUID"},
			wantSQLFragments:  []string{"FROM organizations", "uuid = $1"},
		},
		{
			statement:         consoleOrganizationMapperUpdateByUUIDStatement,
			bound:             buildConsoleOrganizationMapperUpdateByUUID(yourbatis.DialectPostgres, update),
			wantID:            "ConsoleOrganizationMapper.UpdateByUUID",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.Name", "params.Settings", "params.OrgUUID"},
			wantSQLFragments:  []string{"CAST($1 AS text)", "CAST($2 AS jsonb)", "WHERE uuid = $3", "RETURNING"},
		},
		{
			statement:         consoleOrganizationMapperFindProfileByUUIDStatement,
			bound:             buildConsoleOrganizationMapperFindProfileByUUID(yourbatis.DialectPostgres, orgUUID),
			wantID:            "ConsoleOrganizationMapper.FindProfileByUUID",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"orgUUID"},
			wantSQLFragments:  []string{"COALESCE(profile", "uuid = $1"},
		},
		{
			statement:         consoleOrganizationMapperUpdateProfileByUUIDStatement,
			bound:             buildConsoleOrganizationMapperUpdateProfileByUUID(yourbatis.DialectPostgres, profile),
			wantID:            "ConsoleOrganizationMapper.UpdateProfileByUUID",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.Profile", "params.OrgUUID"},
			wantSQLFragments:  []string{"profile = CAST($1 AS jsonb)", "WHERE uuid = $2", "RETURNING"},
		},
	}

	for _, contract := range contracts {
		t.Run(contract.wantID, func(t *testing.T) {
			assertMapperBuilderContract(t, contract)
		})
	}
}

func TestConsoleOrganizationMapperExecution(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: []string{"uuid", "name", "domain", "parent_organization_uuid", "settings", "created_at", "updated_at", "role", "added_at"},
		rows: [][]driver.Value{{
			"11111111-1111-4111-8111-111111111111",
			"Console Organization",
			nil,
			nil,
			[]byte(`{"theme":"dark"}`),
			createdAt,
			updatedAt,
			"admin",
			createdAt,
		}},
	})

	row, err := NewConsoleOrganizationMapper(executor).FindByUUID(
		context.Background(),
		"11111111-1111-4111-8111-111111111111",
	)
	if err != nil || row.UUID == "" || row.ParentOrganizationUUID.Valid {
		t.Fatalf("FindByUUID() = (%+v, %v)", row, err)
	}
	organization, err := row.organizationRecord()
	if err != nil || organization.Settings["theme"] != "dark" {
		t.Fatalf("organizationRecord() = (%+v, %v)", organization, err)
	}

	profileExecutor := newMapperTestExecutor(t, mapperTestResponse{
		columns: []string{"profile"},
		rows:    [][]driver.Value{{[]byte(`{"company_name":"Console"}`)}},
	})
	profile, err := NewConsoleOrganizationMapper(profileExecutor).FindProfileByUUID(
		context.Background(),
		"11111111-1111-4111-8111-111111111111",
	)
	if err != nil || len(profile.Profile) == 0 {
		t.Fatalf("FindProfileByUUID() = (%+v, %v)", profile, err)
	}
}

func TestConsoleOrganizationRowMapsParentUUIDString(t *testing.T) {
	row := consoleOrganizationRow{
		UUID:                   "11111111-1111-4111-8111-111111111111",
		ParentOrganizationUUID: sql.NullString{String: "22222222-2222-4222-8222-222222222222", Valid: true},
		Settings:               []byte(`{}`),
	}
	organization, err := row.organizationRecord()
	if err != nil || organization.ParentOrganizationUUID == nil || *organization.ParentOrganizationUUID != row.ParentOrganizationUUID.String {
		t.Fatalf("organizationRecord() = (%+v, %v)", organization, err)
	}
}

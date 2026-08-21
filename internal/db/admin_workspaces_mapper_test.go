package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superduck-ai/yourbatis"
)

func TestAdminWorkspaceMapperInsert(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	externalKeyID := "key_mapper"
	params := insertAdminWorkspaceParams{
		UUID:             "22222222-2222-4222-8222-222222222222",
		ExternalID:       "wrkspc_mapper",
		OrganizationUUID: "11111111-1111-4111-8111-111111111111",
		Name:             "Mapper workspace",
		CreatedAt:        createdAt,
		CompartmentID:    "compartment_mapper",
		DisplayColor:     "#123456",
		ExternalKeyID:    &externalKeyID,
		Tags:             json.RawMessage(`[{"key":"team","value":"platform"}]`),
	}

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("insert workspace failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewAdminWorkspaceMapper(executor).Insert(context.Background(), params)
		if !errors.Is(err, wantErr) {
			t.Fatalf("Insert() error = %v, want query error", err)
		}
	})

	t.Run("unique violation remains detectable", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			queryErr: &pgconn.PgError{Code: "23505"},
		})
		_, err := NewAdminWorkspaceMapper(executor).Insert(context.Background(), params)
		if !isUniqueViolation(err) {
			t.Fatalf("Insert() error = %v, want detectable unique violation", err)
		}
	})

	t.Run("inserted", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminWorkspaceMapperTestColumns(),
			rows:    [][]driver.Value{adminWorkspaceMapperTestRow(params.ExternalID, createdAt)},
		})
		workspace, err := NewAdminWorkspaceMapper(executor).Insert(context.Background(), params)
		if err != nil || workspace.ExternalID != params.ExternalID || workspace.UUID == uuid.Nil {
			t.Fatalf("Insert() = (%+v, %v)", workspace, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminWorkspaceMapper.Insert",
			yourbatis.StatementInsert,
			[]any{
				params.UUID,
				params.ExternalID,
				params.OrganizationUUID,
				params.Name,
				params.CreatedAt,
				params.CreatedAt,
				params.CompartmentID,
				params.DisplayColor,
				params.ExternalKeyID,
				params.Tags,
			},
			"CAST($10 AS jsonb)",
			"RETURNING",
		)
	})
}

func TestAdminWorkspaceMapperFindByIdentifier(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	workspaceUUID := "22222222-2222-4222-8222-222222222222"
	wantValues := []any{organizationUUID, "wrkspc_mapper", workspaceUUID}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminWorkspaceMapperTestColumns()})
		workspace, err := NewAdminWorkspaceMapper(executor).FindByIdentifier(
			context.Background(),
			organizationUUID,
			"wrkspc_mapper",
			workspaceUUID,
		)
		if !errors.Is(err, sql.ErrNoRows) || workspace.UUID != uuid.Nil {
			t.Fatalf("FindByIdentifier() = (%+v, %v), want zero and sql.ErrNoRows", workspace, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminWorkspaceMapper.FindByIdentifier",
			yourbatis.StatementSelect,
			wantValues,
			"organization_uuid = $1",
			"external_id = $2 OR uuid = $3",
		)
	})

	t.Run("found by external ID", func(t *testing.T) {
		createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminWorkspaceMapperTestColumns(),
			rows:    [][]driver.Value{adminWorkspaceMapperTestRow("wrkspc_mapper", createdAt)},
		})
		workspace, err := NewAdminWorkspaceMapper(executor).FindByIdentifier(
			context.Background(),
			organizationUUID,
			"wrkspc_mapper",
			"",
		)
		if err != nil || workspace.ExternalID != "wrkspc_mapper" {
			t.Fatalf("FindByIdentifier() = (%+v, %v)", workspace, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminWorkspaceMapper.FindByIdentifier",
			yourbatis.StatementSelect,
			[]any{organizationUUID, "wrkspc_mapper"},
			"organization_uuid = $1",
			"external_id = $2",
		)
		if strings.Contains(executor.bound.SQL, "uuid = $3") {
			t.Fatalf("FindByIdentifier() SQL unexpectedly contains UUID filter: %s", executor.bound.SQL)
		}
	})
}

func TestAdminWorkspaceMapperListPageBuildsFilters(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	anchor := &pagePosition{
		CreatedAt: time.Date(2026, time.August, 5, 4, 5, 6, 0, time.UTC),
		UUID:      "22222222-2222-4222-8222-222222222222",
	}
	tests := []struct {
		name            string
		includeArchived bool
		anchor          *pagePosition
		before          bool
		wantValues      []any
		wantSQL         []string
		omitSQL         string
	}{
		{
			name:       "active without cursor",
			wantValues: []any{organizationUUID, 11},
			wantSQL:    []string{"archived_at IS NULL", "LIMIT $2"},
		},
		{
			name:            "archived included",
			includeArchived: true,
			wantValues:      []any{organizationUUID, 11},
			wantSQL:         []string{"LIMIT $2"},
			omitSQL:         "archived_at IS NULL",
		},
		{
			name:       "after anchor",
			anchor:     anchor,
			wantValues: []any{organizationUUID, anchor.CreatedAt, anchor.UUID, 11},
			wantSQL:    []string{"(created_at, uuid) < ($2, $3)", "LIMIT $4"},
		},
		{
			name:       "before anchor",
			anchor:     anchor,
			before:     true,
			wantValues: []any{organizationUUID, anchor.CreatedAt, anchor.UUID, 11},
			wantSQL:    []string{"(created_at, uuid) > ($2, $3)", "LIMIT $4"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bound := buildAdminWorkspaceMapperListPage(
				yourbatis.DialectPostgres,
				organizationUUID,
				test.includeArchived,
				test.anchor,
				test.before,
				11,
			)
			for _, fragment := range test.wantSQL {
				if !strings.Contains(bound.SQL, fragment) {
					t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, fragment)
				}
			}
			if test.omitSQL != "" && strings.Contains(bound.SQL, test.omitSQL) {
				t.Fatalf("generated SQL = %q, omit fragment %q", bound.SQL, test.omitSQL)
			}
			if values := bound.Values(); !reflect.DeepEqual(values, test.wantValues) {
				t.Fatalf("generated arguments = %#v, want %#v", values, test.wantValues)
			}
		})
	}
}

func TestAdminWorkspaceMapperWritesAndCount(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)

	t.Run("update not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminWorkspaceMapperTestColumns()})
		_, err := NewAdminWorkspaceMapper(executor).UpdateByExternalID(
			context.Background(),
			updateAdminWorkspaceParams{
				OrganizationUUID: organizationUUID,
				ExternalID:       "wrkspc_missing",
				Name:             "Missing",
				Tags:             json.RawMessage(`[]`),
				UpdatedAt:        createdAt,
			},
		)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("UpdateByExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("archive not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminWorkspaceMapperTestColumns()})
		_, err := NewAdminWorkspaceMapper(executor).ArchiveByExternalID(
			context.Background(),
			organizationUUID,
			"wrkspc_missing",
		)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("ArchiveByExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("counted", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"count"},
			rows:    [][]driver.Value{{int64(2)}},
		})
		count, err := NewAdminWorkspaceMapper(executor).CountByExternalKeyID(
			context.Background(),
			organizationUUID,
			"key_mapper",
		)
		if err != nil || count != 2 {
			t.Fatalf("CountByExternalKeyID() = (%d, %v), want 2, nil", count, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminWorkspaceMapper.CountByExternalKeyID",
			yourbatis.StatementSelect,
			[]any{organizationUUID, "key_mapper"},
		)
	})
}

func adminWorkspaceMapperTestColumns() []string {
	return []string{
		"uuid",
		"external_id",
		"organization_uuid",
		"name",
		"created_at",
		"updated_at",
		"archived_at",
		"compartment_id",
		"display_color",
		"external_key_id",
		"tags",
	}
}

func adminWorkspaceMapperTestRow(externalID string, createdAt time.Time) []driver.Value {
	return []driver.Value{
		"22222222-2222-4222-8222-222222222222",
		externalID,
		"11111111-1111-4111-8111-111111111111",
		"Mapper workspace",
		createdAt,
		createdAt,
		nil,
		"compartment_mapper",
		"#123456",
		nil,
		[]byte(`[]`),
	}
}

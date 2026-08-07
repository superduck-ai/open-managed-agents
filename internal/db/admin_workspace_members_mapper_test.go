package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superduck-ai/yourbatis"
)

func TestAdminWorkspaceMemberMapperInsert(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	params := insertAdminWorkspaceMemberParams{
		ExternalID:          "wsm_mapper",
		OrganizationUUID:    "11111111-1111-4111-8111-111111111111",
		WorkspaceUUID:       "22222222-2222-4222-8222-222222222222",
		WorkspaceExternalID: "wrkspc_mapper",
		UserUUID:            "33333333-3333-4333-8333-333333333333",
		UserExternalID:      "user_mapper",
		WorkspaceRole:       "workspace_developer",
		CreatedAt:           createdAt,
	}

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("insert member failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewAdminWorkspaceMemberMapper(executor).Insert(context.Background(), params)
		if !errors.Is(err, wantErr) {
			t.Fatalf("Insert() error = %v, want query error", err)
		}
	})

	t.Run("unique violation remains detectable", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			queryErr: &pgconn.PgError{Code: "23505"},
		})
		_, err := NewAdminWorkspaceMemberMapper(executor).Insert(context.Background(), params)
		if !isUniqueViolation(err) {
			t.Fatalf("Insert() error = %v, want detectable unique violation", err)
		}
	})

	t.Run("inserted", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminWorkspaceMemberMapperTestColumns(),
			rows:    [][]driver.Value{adminWorkspaceMemberMapperTestRow(params.UserExternalID, createdAt)},
		})
		member, err := NewAdminWorkspaceMemberMapper(executor).Insert(context.Background(), params)
		if err != nil || member.UserExternalID != params.UserExternalID || member.UUID == uuid.Nil {
			t.Fatalf("Insert() = (%+v, %v)", member, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminWorkspaceMemberMapper.Insert",
			yourbatis.StatementInsert,
			[]any{
				params.ExternalID,
				params.OrganizationUUID,
				params.WorkspaceUUID,
				params.WorkspaceExternalID,
				params.UserUUID,
				params.UserExternalID,
				params.WorkspaceRole,
				params.CreatedAt,
				params.CreatedAt,
			},
			"RETURNING",
		)
	})
}

func TestAdminWorkspaceMemberMapperFindByUserExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "wrkspc_mapper", "user_mapper"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminWorkspaceMemberMapperTestColumns()})
		member, err := NewAdminWorkspaceMemberMapper(executor).FindByUserExternalID(
			context.Background(),
			organizationUUID,
			"wrkspc_mapper",
			"user_mapper",
		)
		if !errors.Is(err, sql.ErrNoRows) || member.UUID != uuid.Nil {
			t.Fatalf("FindByUserExternalID() = (%+v, %v), want zero and sql.ErrNoRows", member, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminWorkspaceMemberMapper.FindByUserExternalID",
			yourbatis.StatementSelect,
			wantValues,
			"deleted_at IS NULL",
		)
	})

	t.Run("found", func(t *testing.T) {
		createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminWorkspaceMemberMapperTestColumns(),
			rows:    [][]driver.Value{adminWorkspaceMemberMapperTestRow("user_mapper", createdAt)},
		})
		member, err := NewAdminWorkspaceMemberMapper(executor).FindByUserExternalID(
			context.Background(),
			organizationUUID,
			"wrkspc_mapper",
			"user_mapper",
		)
		if err != nil || member.UserExternalID != "user_mapper" {
			t.Fatalf("FindByUserExternalID() = (%+v, %v)", member, err)
		}
	})
}

func TestAdminWorkspaceMemberMapperListPageBuildsFilters(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	workspaceUUID := "22222222-2222-4222-8222-222222222222"
	anchor := &pagePosition{
		CreatedAt: time.Date(2026, time.August, 5, 4, 5, 6, 0, time.UTC),
		UUID:      "33333333-3333-4333-8333-333333333333",
	}
	tests := []struct {
		name       string
		anchor     *pagePosition
		before     bool
		wantValues []any
		wantSQL    []string
	}{
		{
			name:       "without cursor",
			wantValues: []any{organizationUUID, workspaceUUID, 11},
			wantSQL:    []string{"deleted_at IS NULL", "LIMIT $3"},
		},
		{
			name:       "after anchor",
			anchor:     anchor,
			wantValues: []any{organizationUUID, workspaceUUID, anchor.CreatedAt, anchor.UUID, 11},
			wantSQL:    []string{"(created_at, uuid) < ($3, $4)", "LIMIT $5"},
		},
		{
			name:       "before anchor",
			anchor:     anchor,
			before:     true,
			wantValues: []any{organizationUUID, workspaceUUID, anchor.CreatedAt, anchor.UUID, 11},
			wantSQL:    []string{"(created_at, uuid) > ($3, $4)", "LIMIT $5"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bound := buildAdminWorkspaceMemberMapperListPage(
				yourbatis.DialectPostgres,
				organizationUUID,
				workspaceUUID,
				test.anchor,
				test.before,
				11,
			)
			for _, fragment := range test.wantSQL {
				if !strings.Contains(bound.SQL, fragment) {
					t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, fragment)
				}
			}
			if values := bound.Values(); !reflect.DeepEqual(values, test.wantValues) {
				t.Fatalf("generated arguments = %#v, want %#v", values, test.wantValues)
			}
		})
	}
}

func TestAdminWorkspaceMemberMapperWrites(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"

	t.Run("update not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminWorkspaceMemberMapperTestColumns()})
		_, err := NewAdminWorkspaceMemberMapper(executor).UpdateRoleByUserExternalID(
			context.Background(),
			updateAdminWorkspaceMemberRoleParams{
				OrganizationUUID:    organizationUUID,
				WorkspaceExternalID: "wrkspc_mapper",
				UserExternalID:      "user_missing",
				WorkspaceRole:       "workspace_admin",
			},
		)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("UpdateRoleByUserExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("delete not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminWorkspaceMemberMapperTestColumns()})
		_, err := NewAdminWorkspaceMemberMapper(executor).SoftDeleteByUserExternalID(
			context.Background(),
			organizationUUID,
			"wrkspc_mapper",
			"user_missing",
		)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("SoftDeleteByUserExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})
}

func adminWorkspaceMemberMapperTestColumns() []string {
	return []string{
		"uuid",
		"external_id",
		"organization_uuid",
		"workspace_uuid",
		"workspace_external_id",
		"user_uuid",
		"user_external_id",
		"workspace_role",
		"created_at",
		"updated_at",
	}
}

func adminWorkspaceMemberMapperTestRow(userExternalID string, createdAt time.Time) []driver.Value {
	return []driver.Value{
		"44444444-4444-4444-8444-444444444444",
		"wsm_mapper",
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"wrkspc_mapper",
		"33333333-3333-4333-8333-333333333333",
		userExternalID,
		"workspace_developer",
		createdAt,
		createdAt,
	}
}

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
	"github.com/superduck-ai/yourbatis"
)

func TestAdminUserMapperFindByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "user_mapper"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminUserMapperTestColumns()})
		user, err := NewAdminUserMapper(executor).FindByExternalID(
			context.Background(),
			organizationUUID,
			"user_mapper",
		)
		if !errors.Is(err, sql.ErrNoRows) || user.UUID != uuid.Nil {
			t.Fatalf("FindByExternalID() = (%+v, %v), want zero and sql.ErrNoRows", user, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.FindByExternalID",
			yourbatis.StatementSelect,
			wantValues,
			"organization_uuid = $1",
			"external_id = $2",
		)
	})

	t.Run("found", func(t *testing.T) {
		addedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminUserMapperTestColumns(),
			rows:    [][]driver.Value{adminUserMapperTestRow("user_mapper", "developer", addedAt)},
		})
		user, err := NewAdminUserMapper(executor).FindByExternalID(
			context.Background(),
			organizationUUID,
			"user_mapper",
		)
		if err != nil || user.ExternalID != "user_mapper" || user.Role != "developer" {
			t.Fatalf("FindByExternalID() = (%+v, %v)", user, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.FindByExternalID",
			yourbatis.StatementSelect,
			wantValues,
		)
	})
}

func TestAdminUserMapperFindPageAnchorByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "user_anchor"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"created_at", "uuid"}})
		anchor, found, err := NewAdminUserMapper(executor).FindPageAnchorByExternalID(
			context.Background(),
			organizationUUID,
			"user_anchor",
		)
		if err != nil || found || anchor.UUID != uuid.Nil {
			t.Fatalf("FindPageAnchorByExternalID() = (%+v, %t, %v), want zero, false, nil", anchor, found, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.FindPageAnchorByExternalID",
			yourbatis.StatementSelect,
			wantValues,
		)
	})

	t.Run("found", func(t *testing.T) {
		addedAt := time.Date(2026, time.August, 5, 2, 3, 4, 0, time.UTC)
		userUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"created_at", "uuid"},
			rows:    [][]driver.Value{{addedAt, userUUID.String()}},
		})
		anchor, found, err := NewAdminUserMapper(executor).FindPageAnchorByExternalID(
			context.Background(),
			organizationUUID,
			"user_anchor",
		)
		if err != nil || !found || anchor.UUID != userUUID || !anchor.CreatedAt.Equal(addedAt) {
			t.Fatalf("FindPageAnchorByExternalID() = (%+v, %t, %v)", anchor, found, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.FindPageAnchorByExternalID",
			yourbatis.StatementSelect,
			wantValues,
			"SELECT added_at AS created_at, uuid",
		)
	})
}

func TestAdminUserMapperListPage(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, 2}

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("list users failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewAdminUserMapper(executor).ListPage(
			context.Background(),
			organizationUUID,
			"",
			nil,
			false,
			2,
		)
		if !errors.Is(err, wantErr) {
			t.Fatalf("ListPage() error = %v, want query error", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.ListPage",
			yourbatis.StatementSelect,
			wantValues,
		)
	})

	t.Run("listed", func(t *testing.T) {
		addedAt := time.Date(2026, time.August, 5, 3, 4, 5, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminUserMapperTestColumns(),
			rows: [][]driver.Value{
				adminUserMapperTestRow("user_newer", "admin", addedAt),
				adminUserMapperTestRow("user_older", "developer", addedAt.Add(-time.Minute)),
			},
		})
		users, err := NewAdminUserMapper(executor).ListPage(
			context.Background(),
			organizationUUID,
			"",
			nil,
			false,
			2,
		)
		if err != nil || len(users) != 2 || users[0].ExternalID != "user_newer" || users[1].ExternalID != "user_older" {
			t.Fatalf("ListPage() = (%+v, %v)", users, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.ListPage",
			yourbatis.StatementSelect,
			wantValues,
			"ORDER BY added_at DESC, uuid DESC",
			"LIMIT $2",
		)
	})
}

func TestAdminUserMapperListPageBuildsFilters(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	anchor := &pagePosition{
		CreatedAt: time.Date(2026, time.August, 5, 4, 5, 6, 0, time.UTC),
		UUID:      uuid.MustParse("22222222-2222-4222-8222-222222222222"),
	}
	tests := []struct {
		name       string
		email      string
		anchor     *pagePosition
		before     bool
		wantValues []any
		wantSQL    string
	}{
		{
			name:       "email",
			email:      "user@example.com",
			wantValues: []any{organizationUUID, "user@example.com", 11},
			wantSQL:    "LOWER(email) = LOWER($2)",
		},
		{
			name:       "after anchor",
			anchor:     anchor,
			wantValues: []any{organizationUUID, anchor.CreatedAt, anchor.UUID, 11},
			wantSQL:    "(added_at, uuid) < ($2, $3)",
		},
		{
			name:       "before anchor",
			anchor:     anchor,
			before:     true,
			wantValues: []any{organizationUUID, anchor.CreatedAt, anchor.UUID, 11},
			wantSQL:    "(added_at, uuid) > ($2, $3)",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bound := buildAdminUserMapperListPage(
				yourbatis.DialectPostgres,
				organizationUUID,
				test.email,
				test.anchor,
				test.before,
				11,
			)
			if !strings.Contains(bound.SQL, test.wantSQL) {
				t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, test.wantSQL)
			}
			if values := bound.Values(); !reflect.DeepEqual(values, test.wantValues) {
				t.Fatalf("generated arguments = %#v, want %#v", values, test.wantValues)
			}
		})
	}
}

func TestAdminUserMapperUpdateRoleByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{"admin", organizationUUID, "user_mapper"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminUserMapperTestColumns()})
		_, err := NewAdminUserMapper(executor).UpdateRoleByExternalID(
			context.Background(),
			organizationUUID,
			"user_mapper",
			"admin",
		)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("UpdateRoleByExternalID() error = %v, want sql.ErrNoRows", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.UpdateRoleByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
		)
	})

	t.Run("updated", func(t *testing.T) {
		addedAt := time.Date(2026, time.August, 5, 5, 6, 7, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminUserMapperTestColumns(),
			rows:    [][]driver.Value{adminUserMapperTestRow("user_mapper", "admin", addedAt)},
		})
		user, err := NewAdminUserMapper(executor).UpdateRoleByExternalID(
			context.Background(),
			organizationUUID,
			"user_mapper",
			"admin",
		)
		if err != nil || user.Role != "admin" {
			t.Fatalf("UpdateRoleByExternalID() = (%+v, %v)", user, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.UpdateRoleByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
			"SET role = $1",
			"organization_uuid = $2",
			"external_id = $3",
		)
	})
}

func TestAdminUserMapperSoftDeleteByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "user_mapper"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminUserMapperTestColumns()})
		_, err := NewAdminUserMapper(executor).SoftDeleteByExternalID(
			context.Background(),
			organizationUUID,
			"user_mapper",
		)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("SoftDeleteByExternalID() error = %v, want sql.ErrNoRows", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.SoftDeleteByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
		)
	})

	t.Run("deleted", func(t *testing.T) {
		addedAt := time.Date(2026, time.August, 5, 6, 7, 8, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminUserMapperTestColumns(),
			rows:    [][]driver.Value{adminUserMapperTestRow("user_mapper", "developer", addedAt)},
		})
		user, err := NewAdminUserMapper(executor).SoftDeleteByExternalID(
			context.Background(),
			organizationUUID,
			"user_mapper",
		)
		if err != nil || user.ExternalID != "user_mapper" {
			t.Fatalf("SoftDeleteByExternalID() = (%+v, %v)", user, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.SoftDeleteByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
			"SET deleted_at = COALESCE(deleted_at, NOW())",
			"deleted_at IS NULL",
		)
	})
}

func TestAdminUserMapperSoftDeleteWorkspaceMembersByUserUUID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	userUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	wantValues := []any{organizationUUID, userUUID}

	t.Run("execution error", func(t *testing.T) {
		wantErr := errors.New("delete workspace memberships failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{execErr: wantErr})
		err := NewAdminUserMapper(executor).SoftDeleteWorkspaceMembersByUserUUID(
			context.Background(),
			organizationUUID,
			userUUID,
		)
		if !errors.Is(err, wantErr) {
			t.Fatalf("SoftDeleteWorkspaceMembersByUserUUID() error = %v, want execution error", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.SoftDeleteWorkspaceMembersByUserUUID",
			yourbatis.StatementUpdate,
			wantValues,
		)
	})

	t.Run("deleted", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{})
		err := NewAdminUserMapper(executor).SoftDeleteWorkspaceMembersByUserUUID(
			context.Background(),
			organizationUUID,
			userUUID,
		)
		if err != nil {
			t.Fatalf("SoftDeleteWorkspaceMembersByUserUUID() error = %v", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminUserMapper.SoftDeleteWorkspaceMembersByUserUUID",
			yourbatis.StatementUpdate,
			wantValues,
			"UPDATE workspace_members",
			"organization_uuid = $1",
			"user_uuid = $2",
		)
	})
}

func adminUserMapperTestColumns() []string {
	return []string{
		"uuid",
		"external_id",
		"organization_uuid",
		"email",
		"name",
		"role",
		"added_at",
	}
}

func adminUserMapperTestRow(externalID, role string, addedAt time.Time) []driver.Value {
	return []driver.Value{
		"22222222-2222-4222-8222-222222222222",
		externalID,
		"11111111-1111-4111-8111-111111111111",
		externalID + "@example.com",
		"Mapper user",
		role,
		addedAt,
	}
}

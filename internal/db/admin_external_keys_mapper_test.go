package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superduck-ai/yourbatis"
)

func TestAdminExternalKeyMapperInsert(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	params := insertAdminExternalKeyParams{
		ExternalID:       "key_external",
		OrganizationUUID: "11111111-1111-4111-8111-111111111111",
		DisplayName:      "External key",
		Geo:              "us",
		ProviderConfig:   json.RawMessage(`{"type":"aws"}`),
		CreatedAt:        createdAt,
	}
	wantValues := []any{
		params.ExternalID,
		params.OrganizationUUID,
		params.DisplayName,
		params.Geo,
		params.ProviderConfig,
		params.CreatedAt,
		params.CreatedAt,
	}

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("insert external key failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewAdminExternalKeyMapper(executor).Insert(context.Background(), params)
		if !errors.Is(err, wantErr) {
			t.Fatalf("Insert() error = %v, want query error", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.Insert",
			yourbatis.StatementInsert,
			wantValues,
		)
	})

	t.Run("unique violation remains detectable", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			queryErr: &pgconn.PgError{Code: "23505"},
		})
		_, err := NewAdminExternalKeyMapper(executor).Insert(context.Background(), params)
		if !isUniqueViolation(err) {
			t.Fatalf("Insert() error = %v, want detectable unique violation", err)
		}
	})

	t.Run("inserted", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminExternalKeyMapperTestColumns(),
			rows:    [][]driver.Value{adminExternalKeyMapperTestRow(params.ExternalID, params.DisplayName, createdAt)},
		})
		key, err := NewAdminExternalKeyMapper(executor).Insert(context.Background(), params)
		if err != nil || key.ExternalID != params.ExternalID || string(key.ProviderConfig) != `{"type":"aws"}` {
			t.Fatalf("Insert() = (%+v, %v)", key, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.Insert",
			yourbatis.StatementInsert,
			wantValues,
			"INSERT INTO external_keys",
			"CAST($5 AS jsonb)",
			"RETURNING",
		)
		assertAdminExternalKeyMapperProviderConfigSensitive(t, executor.bound)
	})
}

func TestAdminExternalKeyMapperFindByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "key_external"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminExternalKeyMapperTestColumns()})
		key, err := NewAdminExternalKeyMapper(executor).FindByExternalID(
			context.Background(),
			organizationUUID,
			"key_external",
		)
		if !errors.Is(err, sql.ErrNoRows) || key.UUID != uuid.Nil {
			t.Fatalf("FindByExternalID() = (%+v, %v), want zero and sql.ErrNoRows", key, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.FindByExternalID",
			yourbatis.StatementSelect,
			wantValues,
			"organization_uuid = $1",
			"external_id = $2",
		)
	})

	t.Run("found", func(t *testing.T) {
		createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminExternalKeyMapperTestColumns(),
			rows:    [][]driver.Value{adminExternalKeyMapperTestRow("key_external", "External key", createdAt)},
		})
		key, err := NewAdminExternalKeyMapper(executor).FindByExternalID(
			context.Background(),
			organizationUUID,
			"key_external",
		)
		if err != nil || key.ExternalID != "key_external" {
			t.Fatalf("FindByExternalID() = (%+v, %v)", key, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.FindByExternalID",
			yourbatis.StatementSelect,
			wantValues,
		)
	})
}

func TestAdminExternalKeyMapperListPage(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, 3, 1}

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("list external keys failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewAdminExternalKeyMapper(executor).ListPage(context.Background(), organizationUUID, 3, 1)
		if !errors.Is(err, wantErr) {
			t.Fatalf("ListPage() error = %v, want query error", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.ListPage",
			yourbatis.StatementSelect,
			wantValues,
		)
	})

	t.Run("listed", func(t *testing.T) {
		createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminExternalKeyMapperTestColumns(),
			rows: [][]driver.Value{
				adminExternalKeyMapperTestRow("key_newer", "Newer", createdAt),
				adminExternalKeyMapperTestRow("key_older", "Older", createdAt.Add(-time.Minute)),
			},
		})
		keys, err := NewAdminExternalKeyMapper(executor).ListPage(context.Background(), organizationUUID, 3, 1)
		if err != nil || len(keys) != 2 || keys[0].ExternalID != "key_newer" || keys[1].ExternalID != "key_older" {
			t.Fatalf("ListPage() = (%+v, %v)", keys, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.ListPage",
			yourbatis.StatementSelect,
			wantValues,
			"ORDER BY created_at DESC, uuid DESC",
			"LIMIT $2",
			"OFFSET $3",
		)
	})
}

func TestAdminExternalKeyMapperUpdateByExternalID(t *testing.T) {
	updatedAt := time.Date(2026, time.August, 5, 2, 3, 4, 0, time.UTC)
	params := updateAdminExternalKeyParams{
		OrganizationUUID: "11111111-1111-4111-8111-111111111111",
		ExternalID:       "key_external",
		DisplayName:      "Updated key",
		Geo:              "us",
		ProviderConfig:   json.RawMessage(`{"type":"gcp"}`),
		UpdatedAt:        updatedAt,
	}
	wantValues := []any{
		params.DisplayName,
		params.Geo,
		params.ProviderConfig,
		params.UpdatedAt,
		params.OrganizationUUID,
		params.ExternalID,
	}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminExternalKeyMapperTestColumns()})
		_, err := NewAdminExternalKeyMapper(executor).UpdateByExternalID(context.Background(), params)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("UpdateByExternalID() error = %v, want sql.ErrNoRows", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.UpdateByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
		)
	})

	t.Run("updated", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminExternalKeyMapperTestColumns(),
			rows:    [][]driver.Value{adminExternalKeyMapperTestRow(params.ExternalID, params.DisplayName, updatedAt)},
		})
		key, err := NewAdminExternalKeyMapper(executor).UpdateByExternalID(context.Background(), params)
		if err != nil || key.DisplayName != params.DisplayName {
			t.Fatalf("UpdateByExternalID() = (%+v, %v)", key, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.UpdateByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
			"provider_config = CAST($3 AS jsonb)",
			"organization_uuid = $5",
			"external_id = $6",
		)
		assertAdminExternalKeyMapperProviderConfigSensitive(t, executor.bound)
	})
}

func TestAdminExternalKeyMapperSoftDeleteByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "key_external"}

	t.Run("execution error", func(t *testing.T) {
		wantErr := errors.New("delete external key failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{execErr: wantErr})
		rowsAffected, err := NewAdminExternalKeyMapper(executor).SoftDeleteByExternalID(
			context.Background(),
			organizationUUID,
			"key_external",
		)
		if rowsAffected != 0 || !errors.Is(err, wantErr) {
			t.Fatalf("SoftDeleteByExternalID() = (%d, %v), want 0 and execution error", rowsAffected, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.SoftDeleteByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
		)
	})

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{})
		rowsAffected, err := NewAdminExternalKeyMapper(executor).SoftDeleteByExternalID(
			context.Background(),
			organizationUUID,
			"key_external",
		)
		if err != nil || rowsAffected != 0 {
			t.Fatalf("SoftDeleteByExternalID() = (%d, %v), want 0, nil", rowsAffected, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.SoftDeleteByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
		)
	})

	t.Run("deleted", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		rowsAffected, err := NewAdminExternalKeyMapper(executor).SoftDeleteByExternalID(
			context.Background(),
			organizationUUID,
			"key_external",
		)
		if err != nil || rowsAffected != 1 {
			t.Fatalf("SoftDeleteByExternalID() = (%d, %v), want 1, nil", rowsAffected, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminExternalKeyMapper.SoftDeleteByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
			"SET deleted_at = COALESCE(deleted_at, NOW())",
			"deleted_at IS NULL",
		)
	})
}

func adminExternalKeyMapperTestColumns() []string {
	return []string{
		"uuid",
		"external_id",
		"organization_uuid",
		"display_name",
		"geo",
		"provider_config",
		"created_at",
		"updated_at",
	}
}

func adminExternalKeyMapperTestRow(externalID, displayName string, createdAt time.Time) []driver.Value {
	return []driver.Value{
		"22222222-2222-4222-8222-222222222222",
		externalID,
		"11111111-1111-4111-8111-111111111111",
		displayName,
		"us",
		[]byte(`{"type":"aws"}`),
		createdAt,
		createdAt.Add(time.Minute),
	}
}

func assertAdminExternalKeyMapperProviderConfigSensitive(t *testing.T, bound yourbatis.BoundSQL) {
	t.Helper()
	for _, argument := range bound.Args {
		wantSensitive := argument.Name == "params.ProviderConfig"
		if argument.Sensitive != wantSensitive {
			t.Fatalf("argument %q sensitive = %t, want %t", argument.Name, argument.Sensitive, wantSensitive)
		}
	}
}

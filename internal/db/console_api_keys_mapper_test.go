package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestConsoleAPIKeyMapperList(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	workspaceUUID := "22222222-2222-4222-8222-222222222222"
	createdAt := time.Date(2026, time.August, 3, 1, 2, 3, 0, time.UTC)
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: consoleAPIKeyMapperTestColumns(),
		rows:    [][]driver.Value{consoleAPIKeyMapperTestRow("apikey_console", createdAt)},
	})

	rows, err := NewConsoleAPIKeyMapper(executor).List(
		context.Background(),
		organizationUUID,
		workspaceUUID,
	)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "apikey_console" || rows[0].WorkspaceAPIKeyUUID == "" {
		t.Fatalf("List() rows = %+v", rows)
	}
	assertMapperTestExecution(
		t,
		executor,
		"ConsoleAPIKeyMapper.List",
		yourbatis.StatementSelect,
		[]any{organizationUUID, workspaceUUID},
		"workspace_uuid = $2",
		"ORDER BY created_at DESC, external_id DESC",
	)
}

func TestConsoleAPIKeyMapperCountUnarchived(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	workspaceUUID := "22222222-2222-4222-8222-222222222222"
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: []string{"count"},
		rows:    [][]driver.Value{{int64(3)}},
	})

	count, err := NewConsoleAPIKeyMapper(executor).CountUnarchived(
		context.Background(),
		organizationUUID,
		workspaceUUID,
	)
	if err != nil || count != 3 {
		t.Fatalf("CountUnarchived() = (%d, %v), want 3, nil", count, err)
	}
	assertMapperTestExecution(
		t,
		executor,
		"ConsoleAPIKeyMapper.CountUnarchived",
		yourbatis.StatementSelect,
		[]any{organizationUUID, workspaceUUID},
		"archived_at IS NULL",
	)
}

func TestConsoleUserMapperExistsActiveByUUID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	creatorUUID := "44444444-4444-4444-8444-444444444444"
	wantValues := []any{organizationUUID, creatorUUID}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"exists"},
			rows:    [][]driver.Value{{false}},
		})
		found, err := NewConsoleUserMapper(executor).ExistsActiveByUUID(
			context.Background(),
			organizationUUID,
			creatorUUID,
		)
		if err != nil || found {
			t.Fatalf("ExistsActiveByUUID() = (%t, %v), want false, nil", found, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"ConsoleUserMapper.ExistsActiveByUUID",
			yourbatis.StatementSelect,
			wantValues,
			"SELECT EXISTS",
			"uuid = $2",
		)
	})

	t.Run("found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"exists"},
			rows:    [][]driver.Value{{true}},
		})
		found, err := NewConsoleUserMapper(executor).ExistsActiveByUUID(
			context.Background(),
			organizationUUID,
			creatorUUID,
		)
		if err != nil || !found {
			t.Fatalf("ExistsActiveByUUID() = (%t, %v), want true, nil", found, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"ConsoleUserMapper.ExistsActiveByUUID",
			yourbatis.StatementSelect,
			wantValues,
		)
	})
}

func TestConsoleAPIKeyMapperInsert(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 2, 3, 4, 0, time.UTC)
	params := consoleAPIKeyMapperTestInsertParams()
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: consoleAPIKeyMapperTestColumns(),
		rows:    [][]driver.Value{consoleAPIKeyMapperTestRow(params.ExternalID, createdAt)},
	})

	row, err := NewConsoleAPIKeyMapper(executor).Insert(context.Background(), params)
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if row.ID != params.ExternalID || row.Status != "active" || !row.CreatedAt.Equal(createdAt) {
		t.Fatalf("Insert() row = %+v", row)
	}
	assertMapperTestExecution(
		t,
		executor,
		"ConsoleAPIKeyMapper.Insert",
		yourbatis.StatementInsert,
		[]any{
			params.ExternalID,
			params.APIKeyUUID,
			params.OrganizationUUID,
			params.WorkspaceUUID,
			params.WorkspaceDisplayID,
			params.Name,
			params.KeyPrefix,
			params.KeySuffix,
			params.KeyHash,
			params.CreatedByUserUUID,
			params.ExpiresAt,
		},
		"INSERT INTO console_api_keys",
		"RETURNING",
	)
}

func TestConsoleAPIKeyMapperUpdateStatus(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 3, 4, 5, 0, time.UTC)
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	workspaceUUID := "22222222-2222-4222-8222-222222222222"
	params := updateConsoleAPIKeyStatusQuery{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		ExternalID:       "apikey_console",
		Status:           "archived",
	}
	rowValues := consoleAPIKeyMapperTestRow(params.ExternalID, createdAt)
	rowValues[8] = "archived"
	rowValues[12] = createdAt.Add(time.Minute)
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: consoleAPIKeyMapperTestColumns(),
		rows:    [][]driver.Value{rowValues},
	})

	row, err := NewConsoleAPIKeyMapper(executor).UpdateStatus(context.Background(), params)
	if err != nil || row.Status != "archived" || row.ArchivedAt == nil {
		t.Fatalf("UpdateStatus() = (%+v, %v)", row, err)
	}
	assertMapperTestExecution(
		t,
		executor,
		"ConsoleAPIKeyMapper.UpdateStatus",
		yourbatis.StatementUpdate,
		[]any{"archived", "archived", organizationUUID, workspaceUUID, "apikey_console"},
		"UPDATE console_api_keys",
		"RETURNING",
	)
}

func consoleAPIKeyMapperTestColumns() []string {
	return []string{
		"id",
		"workspace_api_key_uuid",
		"org_uuid",
		"workspace_uuid",
		"workspace_display_id",
		"name",
		"key_prefix",
		"key_suffix",
		"status",
		"created_by_user_uuid",
		"last_used_at",
		"expires_at",
		"archived_at",
		"created_at",
		"updated_at",
	}
}

func consoleAPIKeyMapperTestRow(externalID string, createdAt time.Time) []driver.Value {
	return []driver.Value{
		externalID,
		"33333333-3333-4333-8333-333333333333",
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"workspace_default",
		"Console key",
		"sk-ant-api03-pre",
		"suffix",
		"active",
		nil,
		nil,
		nil,
		nil,
		createdAt,
		createdAt.Add(time.Minute),
	}
}

func consoleAPIKeyMapperTestInsertParams() insertConsoleAPIKeyQuery {
	return insertConsoleAPIKeyQuery{
		ExternalID:         "apikey_console",
		APIKeyUUID:         "33333333-3333-4333-8333-333333333333",
		OrganizationUUID:   "11111111-1111-4111-8111-111111111111",
		WorkspaceUUID:      "22222222-2222-4222-8222-222222222222",
		WorkspaceDisplayID: "workspace_default",
		Name:               "Console key",
		KeyPrefix:          "sk-ant-api03-pre",
		KeySuffix:          "suffix",
		KeyHash:            "secret-hash",
		CreatedByUserUUID:  nil,
	}
}

func TestConsoleAPIKeyMapperListBuildsOptionalWorkspaceScope(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	workspaceUUID := "22222222-2222-4222-8222-222222222222"

	t.Run("without workspace", func(t *testing.T) {
		bound := buildConsoleAPIKeyMapperList(yourbatis.DialectPostgres, organizationUUID, "")
		if strings.Contains(bound.SQL, "workspace_uuid =") {
			t.Fatalf("generated SQL unexpectedly filters workspace: %q", bound.SQL)
		}
		if values := bound.Values(); !reflect.DeepEqual(values, []any{organizationUUID}) {
			t.Fatalf("generated arguments = %#v, want organization only", values)
		}
	})

	t.Run("with workspace", func(t *testing.T) {
		bound := buildConsoleAPIKeyMapperList(
			yourbatis.DialectPostgres,
			organizationUUID,
			workspaceUUID,
		)
		for _, fragment := range []string{
			"organization_uuid = $1",
			"workspace_uuid = $2",
			"ORDER BY created_at DESC, external_id DESC",
		} {
			if !strings.Contains(bound.SQL, fragment) {
				t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, fragment)
			}
		}
		wantValues := []any{organizationUUID, workspaceUUID}
		if values := bound.Values(); !reflect.DeepEqual(values, wantValues) {
			t.Fatalf("generated arguments = %#v, want %#v", values, wantValues)
		}
	})
}

func TestConsoleAPIKeyMapperInsertMarksCredentialFieldsSensitive(t *testing.T) {
	params := insertConsoleAPIKeyQuery{
		ExternalID:         "apikey_console",
		APIKeyUUID:         "33333333-3333-4333-8333-333333333333",
		OrganizationUUID:   "11111111-1111-4111-8111-111111111111",
		WorkspaceUUID:      "22222222-2222-4222-8222-222222222222",
		WorkspaceDisplayID: "workspace_default",
		Name:               "Console key",
		KeyPrefix:          "sk-ant-api03-pre",
		KeySuffix:          "suffix",
		KeyHash:            "secret-hash",
		CreatedByUserUUID:  nil,
		ExpiresAt:          nil,
	}
	bound := buildConsoleAPIKeyMapperInsert(yourbatis.DialectPostgres, params)

	for _, fragment := range []string{"INSERT INTO console_api_keys", "RETURNING", "created_by_user_ref_uuid"} {
		if !strings.Contains(bound.SQL, fragment) {
			t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, fragment)
		}
	}
	for _, index := range []int{6, 7, 8} {
		if !bound.Args[index].Sensitive {
			t.Fatalf("argument %q is not marked sensitive", bound.Args[index].Name)
		}
	}
	for _, index := range []int{0, 1, 2, 3, 4, 5, 9, 10} {
		if bound.Args[index].Sensitive {
			t.Fatalf("argument %q is unexpectedly marked sensitive", bound.Args[index].Name)
		}
	}
}

func TestConsoleAPIKeyMapperUpdateStatusBuildsScopedUpdate(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	workspaceUUID := "22222222-2222-4222-8222-222222222222"
	bound := buildConsoleAPIKeyMapperUpdateStatus(yourbatis.DialectPostgres, updateConsoleAPIKeyStatusQuery{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		ExternalID:       "apikey_console",
		Status:           "archived",
	})

	for _, fragment := range []string{
		"UPDATE console_api_keys",
		"WHEN $2 = 'archived'",
		"organization_uuid = $3",
		"workspace_uuid = $4",
		"external_id = $5",
		"RETURNING",
	} {
		if !strings.Contains(bound.SQL, fragment) {
			t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, fragment)
		}
	}
	wantValues := []any{"archived", "archived", organizationUUID, workspaceUUID, "apikey_console"}
	if values := bound.Values(); !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("generated arguments = %#v, want %#v", values, wantValues)
	}
}

func TestConsoleAPIKeyRowMapsNullableFields(t *testing.T) {
	creatorUUID := "44444444-4444-4444-8444-444444444444"
	expiresAt := time.Date(2026, time.August, 4, 1, 2, 3, 0, time.UTC)
	key := (consoleAPIKeyRow{
		ID:                "apikey_console",
		OrgUUID:           "11111111-1111-4111-8111-111111111111",
		WorkspaceUUID:     "22222222-2222-4222-8222-222222222222",
		CreatedByUserUUID: sql.NullString{String: creatorUUID, Valid: true},
		ExpiresAt:         &expiresAt,
	}).key()

	if key.CreatedByUserUUID == nil || *key.CreatedByUserUUID != creatorUUID {
		t.Fatalf("created by user UUID = %#v, want %s", key.CreatedByUserUUID, creatorUUID)
	}
	if key.ExpiresAt == nil || !key.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expires at = %#v, want %s", key.ExpiresAt, expiresAt)
	}
}

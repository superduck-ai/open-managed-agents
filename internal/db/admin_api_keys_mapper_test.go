package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/yourbatis"
)

func TestAdminAPIKeyMapperFindByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "api_key_default"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminAPIKeyMapperTestColumns()})
		key, found, err := NewAdminAPIKeyMapper(executor).FindByExternalID(
			context.Background(),
			organizationUUID,
			"api_key_default",
		)
		if err != nil || found || key.UUID != uuid.Nil {
			t.Fatalf("FindByExternalID() = (%+v, %t, %v), want zero, false, nil", key, found, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminAPIKeyMapper.FindByExternalID",
			yourbatis.StatementSelect,
			wantValues,
			"w.organization_uuid = $1",
			"ak.external_id = $2",
		)
	})

	t.Run("found", func(t *testing.T) {
		createdAt := time.Date(2026, time.August, 3, 1, 2, 3, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminAPIKeyMapperTestColumns(),
			rows:    [][]driver.Value{adminAPIKeyMapperTestRow("api_key_default", createdAt)},
		})
		key, found, err := NewAdminAPIKeyMapper(executor).FindByExternalID(
			context.Background(),
			organizationUUID,
			"api_key_default",
		)
		if err != nil || !found {
			t.Fatalf("FindByExternalID() = (%+v, %t, %v)", key, found, err)
		}
		if key.ExternalID != "api_key_default" || !key.CreatedAt.Equal(createdAt) || key.CreatedByUserExternalID == nil {
			t.Fatalf("FindByExternalID() key = %+v", key)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminAPIKeyMapper.FindByExternalID",
			yourbatis.StatementSelect,
			wantValues,
		)
	})
}

func TestAdminAPIKeyMapperFindPageAnchorByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "api_key_anchor"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"created_at", "uuid"}})
		anchor, found, err := NewAdminAPIKeyMapper(executor).FindPageAnchorByExternalID(
			context.Background(),
			organizationUUID,
			"api_key_anchor",
		)
		if err != nil || found || anchor.UUID != uuid.Nil {
			t.Fatalf("FindPageAnchorByExternalID() = (%+v, %t, %v), want zero, false, nil", anchor, found, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminAPIKeyMapper.FindPageAnchorByExternalID",
			yourbatis.StatementSelect,
			wantValues,
		)
	})

	t.Run("found", func(t *testing.T) {
		createdAt := time.Date(2026, time.August, 3, 2, 3, 4, 0, time.UTC)
		keyUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"created_at", "uuid"},
			rows:    [][]driver.Value{{createdAt, keyUUID.String()}},
		})
		anchor, found, err := NewAdminAPIKeyMapper(executor).FindPageAnchorByExternalID(
			context.Background(),
			organizationUUID,
			"api_key_anchor",
		)
		if err != nil || !found || anchor.UUID != keyUUID || !anchor.CreatedAt.Equal(createdAt) {
			t.Fatalf("FindPageAnchorByExternalID() = (%+v, %t, %v)", anchor, found, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminAPIKeyMapper.FindPageAnchorByExternalID",
			yourbatis.StatementSelect,
			wantValues,
		)
	})
}

func TestAdminAPIKeyMapperListPage(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	createdAt := time.Date(2026, time.August, 3, 3, 4, 5, 0, time.UTC)
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: adminAPIKeyMapperTestColumns(),
		rows: [][]driver.Value{
			adminAPIKeyMapperTestRow("api_key_newer", createdAt),
			adminAPIKeyMapperTestRow("api_key_older", createdAt.Add(-time.Minute)),
		},
	})

	keys, err := NewAdminAPIKeyMapper(executor).ListPage(
		context.Background(),
		organizationUUID,
		"",
		"",
		"active",
		nil,
		false,
		2,
	)
	if err != nil {
		t.Fatalf("ListPage() error = %v", err)
	}
	if len(keys) != 2 || keys[0].ExternalID != "api_key_newer" || keys[1].ExternalID != "api_key_older" {
		t.Fatalf("ListPage() keys = %+v", keys)
	}
	assertMapperTestExecution(
		t,
		executor,
		"AdminAPIKeyMapper.ListPage",
		yourbatis.StatementSelect,
		[]any{organizationUUID, "active", 2},
		"ak.status = $2",
		"LIMIT $3",
	)
}

func TestAdminAPIKeyMapperInsert(t *testing.T) {
	params := insertAdminAPIKeyParams{
		UUID:              "33333333-3333-4333-8333-333333333333",
		ExternalID:        "apikey_console",
		WorkspaceUUID:     "22222222-2222-4222-8222-222222222222",
		KeyHash:           "secret-hash",
		CreatedByUserUUID: nil,
		Name:              "Console key",
		PartialKeyHint:    "sk-ant...test",
	}
	wantValues := []any{
		params.UUID,
		params.ExternalID,
		params.WorkspaceUUID,
		params.KeyHash,
		params.CreatedByUserUUID,
		params.Name,
		params.PartialKeyHint,
		params.ExpiresAt,
	}

	t.Run("execution error", func(t *testing.T) {
		wantErr := errors.New("insert API key failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{execErr: wantErr})
		err := NewAdminAPIKeyMapper(executor).Insert(context.Background(), params)
		if !errors.Is(err, wantErr) {
			t.Fatalf("Insert() error = %v, want execution error", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminAPIKeyMapper.Insert",
			yourbatis.StatementInsert,
			wantValues,
		)
	})

	t.Run("inserted", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{})
		if err := NewAdminAPIKeyMapper(executor).Insert(context.Background(), params); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminAPIKeyMapper.Insert",
			yourbatis.StatementInsert,
			wantValues,
			"INSERT INTO api_keys",
		)
	})
}

func TestAdminAPIKeyMapperUpdateByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{"archived", organizationUUID, "api_key_default"}

	t.Run("execution error", func(t *testing.T) {
		wantErr := errors.New("update failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{execErr: wantErr})
		rowsAffected, err := NewAdminAPIKeyMapper(executor).UpdateByExternalID(
			context.Background(),
			organizationUUID,
			"api_key_default",
			false,
			"",
			true,
			"archived",
		)
		if rowsAffected != 0 || !errors.Is(err, wantErr) {
			t.Fatalf("UpdateByExternalID() = (%d, %v), want 0 and execution error", rowsAffected, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminAPIKeyMapper.UpdateByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
		)
	})

	t.Run("updated", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		rowsAffected, err := NewAdminAPIKeyMapper(executor).UpdateByExternalID(
			context.Background(),
			organizationUUID,
			"api_key_default",
			false,
			"",
			true,
			"archived",
		)
		if err != nil || rowsAffected != 1 {
			t.Fatalf("UpdateByExternalID() = (%d, %v), want 1, nil", rowsAffected, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminAPIKeyMapper.UpdateByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
			"status = $1",
			"ak.external_id = $3",
		)
	})
}

func TestAdminAPIKeyMapperUpdateStatusByUUID(t *testing.T) {
	apiKeyUUID := "33333333-3333-4333-8333-333333333333"
	wantValues := []any{"archived", apiKeyUUID}

	t.Run("execution error", func(t *testing.T) {
		wantErr := errors.New("update API key status failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{execErr: wantErr})
		rowsAffected, err := NewAdminAPIKeyMapper(executor).UpdateStatusByUUID(
			context.Background(),
			apiKeyUUID,
			"archived",
		)
		if rowsAffected != 0 || !errors.Is(err, wantErr) {
			t.Fatalf("UpdateStatusByUUID() = (%d, %v), want 0 and execution error", rowsAffected, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminAPIKeyMapper.UpdateStatusByUUID",
			yourbatis.StatementUpdate,
			wantValues,
		)
	})

	t.Run("updated", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		rowsAffected, err := NewAdminAPIKeyMapper(executor).UpdateStatusByUUID(
			context.Background(),
			apiKeyUUID,
			"archived",
		)
		if err != nil || rowsAffected != 1 {
			t.Fatalf("UpdateStatusByUUID() = (%d, %v), want 1, nil", rowsAffected, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminAPIKeyMapper.UpdateStatusByUUID",
			yourbatis.StatementUpdate,
			wantValues,
			"UPDATE api_keys",
			"WHERE uuid = $2",
		)
	})
}

func adminAPIKeyMapperTestColumns() []string {
	return []string{
		"uuid",
		"external_id",
		"workspace_uuid",
		"workspace_external_id",
		"created_by_user_uuid",
		"created_by_user_external_id",
		"name",
		"partial_key_hint",
		"status",
		"created_at",
		"updated_at",
		"expires_at",
	}
}

func adminAPIKeyMapperTestRow(externalID string, createdAt time.Time) []driver.Value {
	return []driver.Value{
		"22222222-2222-4222-8222-222222222222",
		externalID,
		"33333333-3333-4333-8333-333333333333",
		"workspace_default",
		"44444444-4444-4444-8444-444444444444",
		"user_default",
		"Mapper key",
		"sk-ant...test",
		"active",
		createdAt,
		createdAt.Add(time.Minute),
		createdAt.Add(time.Hour),
	}
}

func TestListAdminAPIKeysPageRejectsConflictingCursors(t *testing.T) {
	database := &DB{}
	_, _, err := database.ListAdminAPIKeysPage(context.Background(), ListAdminAPIKeysParams{
		OrganizationUUID: uuid.NewString(),
		AfterID:          "api_key_after",
		BeforeID:         "api_key_before",
		Limit:            10,
	})
	if err == nil || err.Error() != "after_id and before_id cannot be used together" {
		t.Fatalf("ListAdminAPIKeysPage() error = %v", err)
	}
}

func TestAdminAPIKeyMapperFindPageAnchorBuildsScopedLookup(t *testing.T) {
	organizationUUID := "22222222-2222-4222-8222-222222222222"
	bound := buildAdminAPIKeyMapperFindPageAnchorByExternalID(
		yourbatis.DialectPostgres,
		organizationUUID,
		"api_key_anchor",
	)
	for _, fragment := range []string{
		"SELECT ak.created_at, ak.uuid",
		"w.organization_uuid = $1",
		"ak.external_id = $2",
	} {
		if !strings.Contains(bound.SQL, fragment) {
			t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, fragment)
		}
	}
	wantValues := []any{organizationUUID, "api_key_anchor"}
	if values := bound.Values(); !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("generated arguments = %#v, want %#v", values, wantValues)
	}
}

func TestAdminAPIKeyMapperInsertMarksKeyHashSensitive(t *testing.T) {
	params := insertAdminAPIKeyParams{
		UUID:              "33333333-3333-4333-8333-333333333333",
		ExternalID:        "apikey_console",
		WorkspaceUUID:     "22222222-2222-4222-8222-222222222222",
		KeyHash:           "secret-hash",
		CreatedByUserUUID: nil,
		Name:              "Console key",
		PartialKeyHint:    "sk-ant...test",
	}
	bound := buildAdminAPIKeyMapperInsert(yourbatis.DialectPostgres, params)

	if !strings.Contains(bound.SQL, "INSERT INTO api_keys") {
		t.Fatalf("generated SQL = %q, want api_keys insert", bound.SQL)
	}
	for index := range bound.Args {
		wantSensitive := bound.Args[index].Name == "params.KeyHash"
		if bound.Args[index].Sensitive != wantSensitive {
			t.Fatalf("argument %q sensitive = %t, want %t", bound.Args[index].Name, bound.Args[index].Sensitive, wantSensitive)
		}
	}
}

func TestAdminAPIKeyMapperListPageBuildsAfterAnchor(t *testing.T) {
	organizationUUID := "22222222-2222-4222-8222-222222222222"
	anchor := &adminAPIKeyPageAnchor{
		CreatedAt: time.Date(2026, time.July, 1, 2, 3, 4, 0, time.UTC),
		UUID:      uuid.MustParse("33333333-3333-4333-8333-333333333333"),
	}

	bound := buildAdminAPIKeyMapperListPage(
		yourbatis.DialectPostgres,
		organizationUUID,
		"workspace_default",
		"user_default",
		"active",
		anchor,
		false,
		11,
	)

	for _, fragment := range []string{
		"w.organization_uuid = $1",
		"w.external_id = $2",
		"u.external_id = $3",
		"ak.status = $4",
		"(ak.created_at, ak.uuid) < ($5, $6)",
		"ORDER BY ak.created_at DESC, ak.uuid DESC",
		"LIMIT $7",
	} {
		if !strings.Contains(bound.SQL, fragment) {
			t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, fragment)
		}
	}
	wantValues := []any{
		organizationUUID,
		"workspace_default",
		"user_default",
		"active",
		anchor.CreatedAt,
		anchor.UUID,
		11,
	}
	if values := bound.Values(); !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("generated arguments = %#v, want %#v", values, wantValues)
	}
}

func TestAdminAPIKeyMapperListPageBuildsBeforeAnchor(t *testing.T) {
	organizationUUID := "99999999-9999-4999-8999-999999999999"
	anchor := &adminAPIKeyPageAnchor{
		CreatedAt: time.Date(2026, time.August, 1, 2, 3, 4, 0, time.UTC),
		UUID:      uuid.MustParse("88888888-8888-4888-8888-888888888888"),
	}

	bound := buildAdminAPIKeyMapperListPage(
		yourbatis.DialectPostgres,
		organizationUUID,
		"",
		"",
		"",
		anchor,
		true,
		11,
	)

	for _, fragment := range []string{
		"w.organization_uuid = $1",
		"(ak.created_at, ak.uuid) > ($2, $3)",
		"ORDER BY ak.created_at ASC, ak.uuid ASC",
		"LIMIT $4",
	} {
		if !strings.Contains(bound.SQL, fragment) {
			t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, fragment)
		}
	}
	wantValues := []any{organizationUUID, anchor.CreatedAt, anchor.UUID, 11}
	if values := bound.Values(); !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("generated arguments = %#v, want %#v", values, wantValues)
	}
}

func TestAdminAPIKeyMapperUpdateBuildsPartialSet(t *testing.T) {
	organizationUUID := "22222222-2222-4222-8222-222222222222"
	bound := buildAdminAPIKeyMapperUpdateByExternalID(
		yourbatis.DialectPostgres,
		organizationUUID,
		"api_key_default",
		true,
		"Renamed key",
		false,
		"",
	)

	for _, fragment := range []string{
		"SET name = $1,",
		"updated_at = NOW()",
		"w.organization_uuid = $2",
		"ak.external_id = $3",
	} {
		if !strings.Contains(bound.SQL, fragment) {
			t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, fragment)
		}
	}
	if strings.Contains(bound.SQL, "status =") {
		t.Fatalf("generated SQL unexpectedly updates status: %q", bound.SQL)
	}
	if strings.Contains(bound.SQL, "RETURNING") {
		t.Fatalf("generated SQL unexpectedly returns a record: %q", bound.SQL)
	}
	wantValues := []any{"Renamed key", organizationUUID, "api_key_default"}
	if values := bound.Values(); !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("generated arguments = %#v, want %#v", values, wantValues)
	}
}

package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/google/uuid"
	"github.com/superduck-ai/yourbatis"
)

func TestAdminAPIKeyMapperPostgreSQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires project config: %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})).With("component", "database")
	database, err := Open(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("open project database: %v", err)
	}
	defer database.Close()

	tx, err := database.mapperDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatalf("begin mapper fixture transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Errorf("roll back mapper fixture transaction: %v", err)
		}
	}()

	fixture := seedAdminAPIKeyMapperPostgreSQLFixture(t, ctx, tx)
	mapper := NewAdminAPIKeyMapper(tx)

	t.Run("failure enforces organization scope", func(t *testing.T) {
		_, found, err := mapper.FindByExternalID(ctx, fixture.otherOrganizationUUID, fixture.keyIDs[0])
		if err != nil {
			t.Fatalf("FindByExternalID() error = %v", err)
		}
		if found {
			t.Fatal("FindByExternalID() found = true, want false")
		}
	})

	t.Run("failure does not find an unknown page anchor", func(t *testing.T) {
		_, found, err := mapper.FindPageAnchorByExternalID(ctx, fixture.organizationUUID, "apikey_missing")
		if err != nil {
			t.Fatalf("FindPageAnchorByExternalID() error = %v", err)
		}
		if found {
			t.Fatal("FindPageAnchorByExternalID() found = true, want false")
		}
	})

	t.Run("failure update is scoped to the organization", func(t *testing.T) {
		rows, err := mapper.UpdateByExternalID(
			ctx,
			fixture.otherOrganizationUUID,
			fixture.keyIDs[0],
			true,
			"Wrong organization",
			false,
			"",
		)
		if err != nil {
			t.Fatalf("UpdateByExternalID() error = %v", err)
		}
		if rows != 0 {
			t.Fatalf("UpdateByExternalID() rows = %d, want 0", rows)
		}
	})

	t.Run("success finds a fixture API key by external ID", func(t *testing.T) {
		key, found, err := mapper.FindByExternalID(ctx, fixture.organizationUUID, fixture.keyIDs[0])
		if err != nil {
			t.Fatalf("FindByExternalID() error = %v", err)
		}
		if !found {
			t.Fatal("FindByExternalID() found = false, want true")
		}
		if key.ExternalID != fixture.keyIDs[0] ||
			key.WorkspaceExternalID != fixture.workspaceExternalID ||
			key.CreatedByUserExternalID == nil ||
			*key.CreatedByUserExternalID != fixture.userExternalID {
			t.Fatalf("FindByExternalID() key = %+v", key)
		}
	})

	t.Run("success lists fixture API keys in descending order", func(t *testing.T) {
		keys, err := mapper.ListPage(
			ctx,
			fixture.organizationUUID,
			fixture.workspaceExternalID,
			"",
			"",
			nil,
			false,
			len(fixture.keyIDs),
		)
		if err != nil {
			t.Fatalf("ListPage() error = %v", err)
		}
		assertAdminAPIKeyIDs(t, keys, fixture.keyIDs)
	})

	t.Run("success paginates after a fixture API key", func(t *testing.T) {
		anchor := findAdminAPIKeyMapperFixtureAnchor(t, ctx, mapper, fixture, 0)
		keys, err := mapper.ListPage(
			ctx,
			fixture.organizationUUID,
			fixture.workspaceExternalID,
			"",
			"",
			&anchor,
			false,
			1,
		)
		if err != nil {
			t.Fatalf("ListPage() error = %v", err)
		}
		assertAdminAPIKeyIDs(t, keys, fixture.keyIDs[1:2])
	})

	t.Run("success returns the nearest key before a deep page anchor", func(t *testing.T) {
		anchor := findAdminAPIKeyMapperFixtureAnchor(t, ctx, mapper, fixture, 2)
		keys, err := mapper.ListPage(
			ctx,
			fixture.organizationUUID,
			fixture.workspaceExternalID,
			"",
			"",
			&anchor,
			true,
			1,
		)
		if err != nil {
			t.Fatalf("ListPage() error = %v", err)
		}
		assertAdminAPIKeyIDs(t, keys, fixture.keyIDs[1:2])
	})

	t.Run("success updates and reads the fixture API key", func(t *testing.T) {
		rows, err := mapper.UpdateByExternalID(
			ctx,
			fixture.organizationUUID,
			fixture.keyIDs[0],
			true,
			"Renamed fixture key",
			true,
			"inactive",
		)
		if err != nil {
			t.Fatalf("UpdateByExternalID() error = %v", err)
		}
		if rows != 1 {
			t.Fatalf("UpdateByExternalID() rows = %d, want 1", rows)
		}
		key, found, err := mapper.FindByExternalID(ctx, fixture.organizationUUID, fixture.keyIDs[0])
		if err != nil || !found {
			t.Fatalf("FindByExternalID() = (%+v, %t, %v)", key, found, err)
		}
		if key.Name != "Renamed fixture key" || key.Status != "inactive" {
			t.Fatalf("FindByExternalID() key = %+v, want updated name and status", key)
		}
	})

	for _, expected := range []string{
		`level=DEBUG msg="yourbatis statement"`,
		`component=database`,
		`statement=AdminAPIKeyMapper.FindByExternalID`,
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("mapper logs do not contain %q: %s", expected, logs.String())
		}
	}
}

type adminAPIKeyMapperPostgreSQLFixture struct {
	organizationUUID      string
	otherOrganizationUUID string
	workspaceExternalID   string
	userExternalID        string
	keyIDs                []string
}

func seedAdminAPIKeyMapperPostgreSQLFixture(
	t *testing.T,
	ctx context.Context,
	executor yourbatis.Executor,
) adminAPIKeyMapperPostgreSQLFixture {
	t.Helper()
	execMapperFixtureSQL(t, ctx, executor, `
		CREATE TEMPORARY TABLE workspaces (
			uuid uuid PRIMARY KEY,
			external_id text NOT NULL,
			organization_uuid uuid NOT NULL
		) ON COMMIT DROP;
		CREATE TEMPORARY TABLE users (
			uuid uuid PRIMARY KEY,
			external_id text NOT NULL
		) ON COMMIT DROP;
		CREATE TEMPORARY TABLE api_keys (
			uuid uuid PRIMARY KEY,
			external_id text NOT NULL,
			workspace_uuid uuid NOT NULL,
			created_by_user_uuid uuid,
			name text NOT NULL,
			partial_key_hint text NOT NULL,
			status text NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL,
			expires_at timestamptz
		) ON COMMIT DROP
	`)

	fixture := adminAPIKeyMapperPostgreSQLFixture{
		organizationUUID:      "11111111-1111-4111-8111-111111111111",
		otherOrganizationUUID: "22222222-2222-4222-8222-222222222222",
		workspaceExternalID:   "workspace_mapper_fixture",
		userExternalID:        "user_mapper_fixture",
		keyIDs: []string{
			"apikey_mapper_newest",
			"apikey_mapper_newer_middle",
			"apikey_mapper_older_middle",
			"apikey_mapper_oldest",
		},
	}
	workspaceUUID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	otherWorkspaceUUID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	userUUID := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	execMapperFixtureSQL(t, ctx, executor, `
		INSERT INTO workspaces (uuid, external_id, organization_uuid)
		VALUES ($1, $2, $3), ($4, $5, $6)
	`, workspaceUUID, fixture.workspaceExternalID, fixture.organizationUUID,
		otherWorkspaceUUID, "workspace_mapper_other", fixture.otherOrganizationUUID)
	execMapperFixtureSQL(t, ctx, executor, `
		INSERT INTO users (uuid, external_id)
		VALUES ($1, $2)
	`, userUUID, fixture.userExternalID)

	keyUUIDs := []uuid.UUID{
		uuid.MustParse("66666666-6666-4666-8666-666666666666"),
		uuid.MustParse("77777777-7777-4777-8777-777777777777"),
		uuid.MustParse("88888888-8888-4888-8888-888888888888"),
		uuid.MustParse("99999999-9999-4999-8999-999999999999"),
	}
	baseCreatedAt := time.Date(2026, time.August, 2, 1, 2, 3, 0, time.UTC)
	for index, externalID := range fixture.keyIDs {
		createdByUserUUID := any(userUUID)
		if index == len(fixture.keyIDs)-1 {
			createdByUserUUID = nil
		}
		createdAt := baseCreatedAt.Add(-time.Duration(index) * time.Second)
		execMapperFixtureSQL(t, ctx, executor, `
			INSERT INTO api_keys (
				uuid, external_id, workspace_uuid, created_by_user_uuid,
				name, partial_key_hint, status, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, 'active', $7, $7)
		`, keyUUIDs[index], externalID, workspaceUUID, createdByUserUUID,
			"Fixture "+externalID, "fixture...key", createdAt)
	}
	return fixture
}

func execMapperFixtureSQL(
	t *testing.T,
	ctx context.Context,
	executor yourbatis.Executor,
	query string,
	values ...any,
) {
	t.Helper()
	arguments := make([]yourbatis.Argument, len(values))
	for index, value := range values {
		arguments[index] = yourbatis.Argument{Value: value}
	}
	_, err := executor.Exec(ctx, yourbatis.Statement{
		ID:     "AdminAPIKeyMapperTest.Fixture",
		Source: "admin_api_keys_mapper_postgres_test.go",
		Kind:   yourbatis.StatementInsert,
	}, yourbatis.BoundSQL{SQL: query, Args: arguments})
	if err != nil {
		t.Fatalf("execute mapper fixture SQL: %v", err)
	}
}

func findAdminAPIKeyMapperFixtureAnchor(
	t *testing.T,
	ctx context.Context,
	mapper AdminAPIKeyMapper,
	fixture adminAPIKeyMapperPostgreSQLFixture,
	index int,
) adminAPIKeyPageAnchor {
	t.Helper()
	anchor, found, err := mapper.FindPageAnchorByExternalID(ctx, fixture.organizationUUID, fixture.keyIDs[index])
	if err != nil || !found {
		t.Fatalf("FindPageAnchorByExternalID() = (%+v, %t, %v)", anchor, found, err)
	}
	return anchor
}

func assertAdminAPIKeyIDs(t *testing.T, keys []AdminAPIKey, wantIDs []string) {
	t.Helper()
	if len(keys) != len(wantIDs) {
		t.Fatalf("API keys = %+v, want IDs %v", keys, wantIDs)
	}
	for index, wantID := range wantIDs {
		if keys[index].ExternalID != wantID {
			t.Fatalf("API key[%d] ID = %q, want %q", index, keys[index].ExternalID, wantID)
		}
	}
}

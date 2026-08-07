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

	"github.com/superduck-ai/yourbatis"
)

func TestConsoleAPIKeyMapperPostgreSQL(t *testing.T) {
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
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("roll back mapper fixture transaction: %v", rollbackErr)
		}
	}()

	fixture := seedConsoleAPIKeyMapperPostgreSQLFixture(t, ctx, tx)
	consoleMapper := NewConsoleAPIKeyMapper(tx)
	userMapper := NewConsoleUserMapper(tx)
	adminMapper := NewAdminAPIKeyMapper(tx)

	t.Run("failure does not resolve a creator from another organization", func(t *testing.T) {
		found, findErr := userMapper.ExistsActiveByUUID(
			ctx,
			fixture.otherOrganizationUUID,
			fixture.creatorUUID,
		)
		if findErr != nil {
			t.Fatalf("ExistsActiveByUUID() error = %v", findErr)
		}
		if found {
			t.Fatal("ExistsActiveByUUID() = true, want false")
		}
	})

	t.Run("failure does not update an unknown Console API key", func(t *testing.T) {
		_, updateErr := consoleMapper.UpdateStatus(ctx, updateConsoleAPIKeyStatusQuery{
			OrganizationUUID: fixture.organizationUUID,
			WorkspaceUUID:    fixture.workspaceUUID,
			ExternalID:       "apikey_console_missing",
			Status:           "archived",
		})
		if !errors.Is(updateErr, sql.ErrNoRows) {
			t.Fatalf("UpdateStatus() error = %v, want sql.ErrNoRows", updateErr)
		}
	})

	t.Run("success creates lists counts and archives both API key records", func(t *testing.T) {
		found, findErr := userMapper.ExistsActiveByUUID(
			ctx,
			fixture.organizationUUID,
			fixture.creatorUUID,
		)
		if findErr != nil || !found {
			t.Fatalf("ExistsActiveByUUID() = (%t, %v), want true, nil", found, findErr)
		}

		apiKeyUUID := "55555555-5555-4555-8555-555555555555"
		expiresAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		creatorUUID := fixture.creatorUUID
		creator := &creatorUUID
		row, insertErr := consoleMapper.Insert(ctx, insertConsoleAPIKeyQuery{
			ExternalID:         fixture.apiKeyExternalID,
			APIKeyUUID:         apiKeyUUID,
			OrganizationUUID:   fixture.organizationUUID,
			WorkspaceUUID:      fixture.workspaceUUID,
			WorkspaceDisplayID: fixture.workspaceDisplayID,
			Name:               "Console mapper fixture",
			KeyPrefix:          "sk-ant-api03-map",
			KeySuffix:          "suffix",
			KeyHash:            "fixture-hash",
			CreatedByUserUUID:  creator,
			ExpiresAt:          &expiresAt,
		})
		if insertErr != nil {
			t.Fatalf("Insert() error = %v", insertErr)
		}
		if row.ID != fixture.apiKeyExternalID ||
			!row.CreatedByUserUUID.Valid ||
			row.CreatedByUserUUID.String != fixture.creatorUUID ||
			row.ExpiresAt == nil {
			t.Fatalf("Insert() row = %+v", row)
		}
		if insertErr = adminMapper.Insert(ctx, insertAdminAPIKeyParams{
			UUID:              apiKeyUUID,
			ExternalID:        fixture.apiKeyExternalID,
			WorkspaceUUID:     fixture.workspaceUUID,
			KeyHash:           "fixture-hash",
			CreatedByUserUUID: creator,
			Name:              "Console mapper fixture",
			PartialKeyHint:    "fixture...hash",
			ExpiresAt:         &expiresAt,
		}); insertErr != nil {
			t.Fatalf("AdminAPIKeyMapper.Insert() error = %v", insertErr)
		}

		rows, listErr := consoleMapper.List(
			ctx,
			fixture.organizationUUID,
			fixture.workspaceUUID,
		)
		if listErr != nil || len(rows) != 1 || rows[0].ID != fixture.apiKeyExternalID {
			t.Fatalf("List() = (%+v, %v)", rows, listErr)
		}
		count, countErr := consoleMapper.CountUnarchived(ctx, fixture.organizationUUID, fixture.workspaceUUID)
		if countErr != nil || count != 1 {
			t.Fatalf("CountUnarchived() = (%d, %v), want 1", count, countErr)
		}

		updated, updateErr := consoleMapper.UpdateStatus(ctx, updateConsoleAPIKeyStatusQuery{
			OrganizationUUID: fixture.organizationUUID,
			WorkspaceUUID:    fixture.workspaceUUID,
			ExternalID:       fixture.apiKeyExternalID,
			Status:           "archived",
		})
		if updateErr != nil || updated.Status != "archived" || updated.ArchivedAt == nil {
			t.Fatalf("UpdateStatus() = (%+v, %v)", updated, updateErr)
		}
		rowsAffected, updateErr := adminMapper.UpdateStatusByUUID(ctx, apiKeyUUID, "archived")
		if updateErr != nil || rowsAffected != 1 {
			t.Fatalf("AdminAPIKeyMapper.UpdateStatusByUUID() = (%d, %v), want 1", rowsAffected, updateErr)
		}
		count, countErr = consoleMapper.CountUnarchived(ctx, fixture.organizationUUID, fixture.workspaceUUID)
		if countErr != nil || count != 0 {
			t.Fatalf("CountUnarchived() after archive = (%d, %v), want 0", count, countErr)
		}
		if status := queryConsoleAPIKeyMapperWorkspaceAPIKeyStatus(t, ctx, tx, fixture.apiKeyExternalID); status != "archived" {
			t.Fatalf("workspace API key status = %q, want archived", status)
		}
	})

	for _, expected := range []string{
		`component=database`,
		`statement=ConsoleUserMapper.ExistsActiveByUUID`,
		`statement=ConsoleAPIKeyMapper.List`,
		`statement=AdminAPIKeyMapper.UpdateStatusByUUID`,
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("mapper logs do not contain %q: %s", expected, logs.String())
		}
	}
}

type consoleAPIKeyMapperPostgreSQLFixture struct {
	organizationUUID      string
	otherOrganizationUUID string
	workspaceUUID         string
	workspaceDisplayID    string
	creatorUUID           string
	creatorExternalID     string
	apiKeyExternalID      string
}

func seedConsoleAPIKeyMapperPostgreSQLFixture(
	t *testing.T,
	ctx context.Context,
	executor yourbatis.Executor,
) consoleAPIKeyMapperPostgreSQLFixture {
	t.Helper()
	fixture := consoleAPIKeyMapperPostgreSQLFixture{
		organizationUUID:      "11111111-1111-4111-8111-111111111111",
		otherOrganizationUUID: "22222222-2222-4222-8222-222222222222",
		workspaceUUID:         "33333333-3333-4333-8333-333333333333",
		workspaceDisplayID:    "workspace_console_fixture",
		creatorUUID:           "44444444-4444-4444-8444-444444444444",
		creatorExternalID:     "user_console_fixture",
		apiKeyExternalID:      "apikey_console_fixture",
	}
	execMapperFixtureSQL(t, ctx, executor, `
		CREATE TEMPORARY TABLE users (
			uuid uuid PRIMARY KEY,
			external_id text NOT NULL,
			organization_uuid uuid NOT NULL,
			deleted_at timestamptz
		) ON COMMIT DROP;
		CREATE TEMPORARY TABLE console_api_keys (
			external_id text PRIMARY KEY,
			api_key_ref_uuid uuid NOT NULL UNIQUE,
			organization_uuid uuid NOT NULL,
			workspace_uuid uuid NOT NULL,
			workspace_display_id text NOT NULL,
			name text NOT NULL,
			key_prefix text NOT NULL,
			key_suffix text NOT NULL,
			key_hash text NOT NULL,
			status text NOT NULL DEFAULT 'active',
			created_by_user_ref_uuid uuid,
			last_used_at timestamptz,
			expires_at timestamptz,
			archived_at timestamptz,
			created_at timestamptz NOT NULL DEFAULT NOW(),
			updated_at timestamptz NOT NULL DEFAULT NOW()
		) ON COMMIT DROP;
		CREATE TEMPORARY TABLE api_keys (
			uuid uuid PRIMARY KEY,
			external_id text NOT NULL UNIQUE,
			workspace_uuid uuid NOT NULL,
			key_hash text NOT NULL,
			status text NOT NULL,
			created_by_user_uuid uuid,
			name text NOT NULL,
			partial_key_hint text NOT NULL,
			expires_at timestamptz,
			updated_at timestamptz NOT NULL DEFAULT NOW()
		) ON COMMIT DROP
	`)
	execMapperFixtureSQL(t, ctx, executor, `
		INSERT INTO users (uuid, external_id, organization_uuid)
		VALUES ($1, $2, $3)
	`, fixture.creatorUUID, fixture.creatorExternalID, fixture.organizationUUID)
	return fixture
}

func queryConsoleAPIKeyMapperWorkspaceAPIKeyStatus(
	t *testing.T,
	ctx context.Context,
	executor yourbatis.Executor,
	externalID string,
) string {
	t.Helper()
	rows, err := executor.Query(ctx, yourbatis.Statement{
		ID:     "ConsoleAPIKeyMapperTest.WorkspaceAPIKeyStatus",
		Source: "console_api_keys_mapper_postgres_test.go",
		Kind:   yourbatis.StatementSelect,
	}, yourbatis.BoundSQL{
		SQL:  "SELECT status FROM api_keys WHERE external_id = $1",
		Args: []yourbatis.Argument{{Name: "externalID", Value: externalID}},
	})
	if err != nil {
		t.Fatalf("query workspace API key status: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("query workspace API key status returned no rows: %v", rows.Err())
	}
	var status string
	if err := rows.Scan(&status); err != nil {
		t.Fatalf("scan workspace API key status: %v", err)
	}
	return status
}

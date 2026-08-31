package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestAdminExternalKeyMapperPostgreSQL(t *testing.T) {
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

	execMapperFixtureSQL(t, ctx, tx, `
		CREATE TEMPORARY TABLE external_keys (
			uuid uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			external_id text NOT NULL UNIQUE,
			organization_uuid uuid NOT NULL,
			display_name text NOT NULL,
			geo text NOT NULL,
			provider_config jsonb NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL,
			deleted_at timestamptz
		) ON COMMIT DROP
	`)
	mapper := NewAdminExternalKeyMapper(tx)
	organizationUUID := "11111111-1111-4111-8111-111111111111"

	t.Run("failure does not find an unknown external key", func(t *testing.T) {
		_, findErr := mapper.FindByExternalID(ctx, organizationUUID, "key_missing")
		if !errors.Is(findErr, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() error = %v, want sql.ErrNoRows", findErr)
		}
	})

	t.Run("failure does not update an unknown external key", func(t *testing.T) {
		_, updateErr := mapper.UpdateByExternalID(ctx, updateAdminExternalKeyParams{
			OrganizationUUID: organizationUUID,
			ExternalID:       "key_missing",
			DisplayName:      "Missing",
			Geo:              "us",
			ProviderConfig:   json.RawMessage(`{"type":"aws"}`),
			UpdatedAt:        time.Now().UTC(),
		})
		if !errors.Is(updateErr, sql.ErrNoRows) {
			t.Fatalf("UpdateByExternalID() error = %v, want sql.ErrNoRows", updateErr)
		}
	})

	t.Run("failure does not delete an unknown external key", func(t *testing.T) {
		rowsAffected, deleteErr := mapper.SoftDeleteByExternalID(ctx, organizationUUID, "key_missing")
		if deleteErr != nil || rowsAffected != 0 {
			t.Fatalf("SoftDeleteByExternalID() = (%d, %v), want 0, nil", rowsAffected, deleteErr)
		}
	})

	t.Run("success creates reads lists updates and deletes JSON configuration", func(t *testing.T) {
		createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		created, createErr := mapper.Insert(ctx, insertAdminExternalKeyParams{
			ExternalID:       "key_mapper_fixture",
			OrganizationUUID: organizationUUID,
			DisplayName:      "Mapper external key",
			Geo:              "us",
			ProviderConfig:   json.RawMessage(`{"type":"aws","region":"us-east-1"}`),
			CreatedAt:        createdAt,
		})
		if createErr != nil || created.ExternalID != "key_mapper_fixture" || created.UUID == "" {
			t.Fatalf("Insert() = (%+v, %v)", created, createErr)
		}
		assertAdminExternalKeyProviderType(t, created.ProviderConfig, "aws")

		foundKey, findErr := mapper.FindByExternalID(ctx, organizationUUID, created.ExternalID)
		if findErr != nil || foundKey.UUID != created.UUID {
			t.Fatalf("FindByExternalID() = (%+v, %v)", foundKey, findErr)
		}

		keys, listErr := mapper.ListPage(ctx, organizationUUID, 2, 0)
		if listErr != nil || len(keys) != 1 || keys[0].UUID != created.UUID {
			t.Fatalf("ListPage() = (%+v, %v)", keys, listErr)
		}

		updated, updateErr := mapper.UpdateByExternalID(ctx, updateAdminExternalKeyParams{
			OrganizationUUID: organizationUUID,
			ExternalID:       created.ExternalID,
			DisplayName:      "Updated mapper external key",
			Geo:              "us",
			ProviderConfig:   json.RawMessage(`{"type":"gcp","key_name":"fixture"}`),
			UpdatedAt:        createdAt.Add(time.Minute),
		})
		if updateErr != nil || updated.DisplayName != "Updated mapper external key" {
			t.Fatalf("UpdateByExternalID() = (%+v, %v)", updated, updateErr)
		}
		assertAdminExternalKeyProviderType(t, updated.ProviderConfig, "gcp")

		rowsAffected, deleteErr := mapper.SoftDeleteByExternalID(ctx, organizationUUID, created.ExternalID)
		if deleteErr != nil || rowsAffected != 1 {
			t.Fatalf("SoftDeleteByExternalID() = (%d, %v), want 1, nil", rowsAffected, deleteErr)
		}
		_, findErr = mapper.FindByExternalID(ctx, organizationUUID, created.ExternalID)
		if !errors.Is(findErr, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() after delete error = %v, want sql.ErrNoRows", findErr)
		}
	})

	for _, expected := range []string{
		`component=database`,
		`statement=AdminExternalKeyMapper.Insert`,
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("mapper logs do not contain %q: %s", expected, logs.String())
		}
	}
}

func assertAdminExternalKeyProviderType(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	var provider struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &provider); err != nil || provider.Type != want {
		t.Fatalf("provider_config = %s, want type %q (error: %v)", raw, want, err)
	}
}

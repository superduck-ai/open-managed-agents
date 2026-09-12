package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestMCPTunnelTokenMapperPostgreSQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires project config: %v", err)
	}
	database, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tx, err := database.mapperDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	}()
	execMapperFixtureSQL(t, ctx, tx, `
		CREATE TEMPORARY TABLE mcp_tunnels (
			uuid uuid PRIMARY KEY, external_id text, organization_uuid uuid,
			workspace_uuid uuid, archived_at timestamptz
		) ON COMMIT DROP;
		CREATE TEMPORARY TABLE mcp_tunnel_token_versions (
			uuid uuid PRIMARY KEY, external_id text, tunnel_uuid uuid,
			version bigint, token_hash bytea, ciphertext bytea, nonce bytea,
			wrapped_dek bytea, format_version integer, key_provider text,
			key_version bigint, created_at timestamptz, retired_at timestamptz,
			archived_at timestamptz
		) ON COMMIT DROP;
		INSERT INTO mcp_tunnels (uuid, external_id, organization_uuid, workspace_uuid)
		VALUES ('11111111-1111-4111-8111-111111111111', 'tunnel_fixture',
		'22222222-2222-4222-8222-222222222222', '33333333-3333-4333-8333-333333333333')
	`)
	mapper := NewMCPTunnelTokenMapper(tx)
	tunnelUUID := "11111111-1111-4111-8111-111111111111"
	assertMissing := func(t *testing.T) {
		t.Helper()
		if _, found, err := mapper.FindActiveByTunnelUUID(ctx, tunnelUUID); err != nil || found {
			t.Fatalf("active lookup = (%t, %v), want false, nil", found, err)
		}
		if _, found, err := mapper.FindActiveForUpdate(ctx, tunnelUUID); err != nil || found {
			t.Fatalf("locking lookup = (%t, %v), want false, nil", found, err)
		}
	}
	t.Run("missing token", func(t *testing.T) {
		assertMissing(t)
		if _, found, err := mapper.FindByHashAndTunnelExternalID(ctx, []byte("hash"), "tunnel_fixture"); err != nil || found {
			t.Fatalf("credential lookup = (%t, %v), want false, nil", found, err)
		}
	})
	now := time.Now().UTC()
	_, err = mapper.Insert(ctx, insertMCPTunnelTokenParams{
		UUID: "44444444-4444-4444-8444-444444444444", ExternalID: "ttkn_fixture", TunnelUUID: tunnelUUID,
		Version: 1, TokenHash: []byte("hash"), Ciphertext: []byte("ciphertext"), Nonce: []byte("nonce"),
		WrappedDEK: []byte("wrapped"), FormatVersion: 1, KeyProvider: "local", KeyVersion: 1, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("wrong tunnel cannot use token", func(t *testing.T) {
		if _, found, err := mapper.FindByHashAndTunnelExternalID(ctx, []byte("hash"), "tunnel_other"); err != nil || found {
			t.Fatalf("wrong tunnel lookup = (%t, %v), want false, nil", found, err)
		}
	})
	t.Run("active token", func(t *testing.T) {
		active, found, err := mapper.FindActiveByTunnelUUID(ctx, tunnelUUID)
		if err != nil || !found || active.ExternalID != "ttkn_fixture" || !active.FormatVersion.Valid {
			t.Fatalf("active token = (%+v, %t, %v)", active, found, err)
		}
		locked, found, err := mapper.FindActiveForUpdate(ctx, tunnelUUID)
		if err != nil || !found || locked.UUID != active.UUID {
			t.Fatalf("locked token = (%+v, %t, %v)", locked, found, err)
		}
	})
	t.Run("retired token retains credential context", func(t *testing.T) {
		if rows, err := mapper.RetireActiveByTunnelUUID(ctx, tunnelUUID, now); err != nil || rows != 1 {
			t.Fatalf("retire = (%d, %v)", rows, err)
		}
		assertMissing(t)
		credential, found, err := mapper.FindByHashAndTunnelExternalID(ctx, []byte("hash"), "tunnel_fixture")
		if err != nil || !found || credential.RetiredAt == nil || credential.FormatVersion.Valid || credential.Ciphertext != nil {
			t.Fatalf("retired context = (%+v, %t, %v)", credential, found, err)
		}
		if credential.OrganizationUUID != "22222222-2222-4222-8222-222222222222" || credential.WorkspaceUUID != "33333333-3333-4333-8333-333333333333" {
			t.Fatal("credential context lost tenant scope")
		}
	})
}

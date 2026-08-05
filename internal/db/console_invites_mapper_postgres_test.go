package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestConsoleInviteMapperPostgreSQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires project config: %v", err)
	}
	database, err := Open(ctx, cfg, nil)
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
		CREATE TEMPORARY TABLE organization_invites (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			uuid uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
			external_id text NOT NULL UNIQUE,
			organization_uuid uuid NOT NULL,
			email text NOT NULL,
			role text NOT NULL,
			status text NOT NULL,
			invited_at timestamptz NOT NULL,
			expires_at timestamptz NOT NULL,
			deleted_at timestamptz
		) ON COMMIT DROP
	`)

	mapper := NewConsoleInviteMapper(tx)
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	otherOrganizationUUID := "22222222-2222-4222-8222-222222222222"
	invitedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)

	t.Run("failure keeps unknown and cross-organization invites isolated", func(t *testing.T) {
		_, resendErr := mapper.ResendByExternalID(ctx, resendConsoleInviteParams{
			OrganizationUUID: organizationUUID,
			ExternalID:       "invite_missing",
			InvitedAt:        invitedAt,
			ExpiresAt:        invitedAt.Add(21 * 24 * time.Hour),
		})
		if !errors.Is(resendErr, sql.ErrNoRows) {
			t.Fatalf("ResendByExternalID() error = %v, want sql.ErrNoRows", resendErr)
		}

		_, insertErr := mapper.Insert(ctx, insertConsoleInviteParams{
			ExternalID:       "invite_other_org",
			OrganizationUUID: otherOrganizationUUID,
			Email:            "other@example.com",
			Role:             "developer",
			InvitedAt:        invitedAt,
			ExpiresAt:        invitedAt.Add(21 * 24 * time.Hour),
		})
		if insertErr != nil {
			t.Fatalf("insert other organization invite: %v", insertErr)
		}
		rows, listErr := mapper.List(ctx, organizationUUID, "", 100)
		if listErr != nil || len(rows) != 0 {
			t.Fatalf("List() cross-organization rows = (%+v, %v), want empty", rows, listErr)
		}
		_, resendErr = mapper.ResendByExternalID(ctx, resendConsoleInviteParams{
			OrganizationUUID: organizationUUID,
			ExternalID:       "invite_other_org",
			InvitedAt:        invitedAt,
			ExpiresAt:        invitedAt.Add(21 * 24 * time.Hour),
		})
		if !errors.Is(resendErr, sql.ErrNoRows) {
			t.Fatalf("ResendByExternalID() cross-organization error = %v, want sql.ErrNoRows", resendErr)
		}
	})

	t.Run("success creates filters resends and deletes invites", func(t *testing.T) {
		created, createErr := mapper.Insert(ctx, insertConsoleInviteParams{
			ExternalID:       "invite_pending",
			OrganizationUUID: organizationUUID,
			Email:            "pending@example.com",
			Role:             "billing",
			InvitedAt:        invitedAt,
			ExpiresAt:        time.Now().UTC().Add(21 * 24 * time.Hour),
		})
		if createErr != nil || created.ID != "invite_pending" || created.Status != "pending" {
			t.Fatalf("Insert() = (%+v, %v)", created, createErr)
		}
		execMapperFixtureSQL(t, ctx, tx, `
			INSERT INTO organization_invites (
				external_id, organization_uuid, email, role, status, invited_at, expires_at, deleted_at
			) VALUES
				('invite_expired', $1, 'expired@example.com', 'developer', 'pending', $2, NOW() - INTERVAL '1 hour', NULL),
				('invite_accepted', $1, 'accepted@example.com', 'developer', 'accepted', $2, NOW() + INTERVAL '1 hour', NULL),
				('invite_deleted', $1, 'deleted@example.com', 'developer', 'deleted', $2, NOW() + INTERVAL '1 hour', NOW())
		`, organizationUUID, invitedAt.Add(-time.Hour))

		for _, test := range []struct {
			status string
			wantID string
		}{
			{status: "pending", wantID: "invite_pending"},
			{status: "expired", wantID: "invite_expired"},
			{status: "accepted", wantID: "invite_accepted"},
			{status: "deleted", wantID: "invite_deleted"},
		} {
			rows, listErr := mapper.List(ctx, organizationUUID, test.status, 100)
			if listErr != nil || len(rows) != 1 || rows[0].ID != test.wantID {
				t.Fatalf("List(%q) = (%+v, %v), want %q", test.status, rows, listErr, test.wantID)
			}
		}

		resentAt := invitedAt.Add(time.Hour)
		resent, resendErr := mapper.ResendByExternalID(ctx, resendConsoleInviteParams{
			OrganizationUUID: organizationUUID,
			ExternalID:       "invite_expired",
			InvitedAt:        resentAt,
			ExpiresAt:        time.Now().UTC().Add(21 * 24 * time.Hour),
		})
		if resendErr != nil || resent.Status != "pending" || !resent.InvitedAt.Equal(resentAt) {
			t.Fatalf("ResendByExternalID() = (%+v, %v)", resent, resendErr)
		}
		deleted, deleteErr := mapper.SoftDeleteByExternalID(ctx, organizationUUID, "invite_pending")
		if deleteErr != nil || deleted.Status != "deleted" {
			t.Fatalf("SoftDeleteByExternalID() = (%+v, %v)", deleted, deleteErr)
		}
		pending, listErr := mapper.List(ctx, organizationUUID, "pending", 100)
		if listErr != nil || len(pending) != 1 || pending[0].ID != "invite_expired" {
			t.Fatalf("List(pending) after updates = (%+v, %v)", pending, listErr)
		}
	})
}

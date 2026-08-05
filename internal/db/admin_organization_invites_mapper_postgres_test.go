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
)

func TestAdminInviteMapperPostgreSQL(t *testing.T) {
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
		CREATE TEMPORARY TABLE organization_invites (
			uuid uuid PRIMARY KEY DEFAULT gen_random_uuid(),
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
	scopeOrganizationUUID := "11111111-1111-4111-8111-111111111111"
	organizationUUID := "22222222-2222-4222-8222-222222222222"
	baseInvitedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	execMapperFixtureSQL(t, ctx, tx, `
		INSERT INTO organization_invites (
			external_id, organization_uuid, email, role, status, invited_at, expires_at
		) VALUES ($1, $2, $3, 'user', 'pending', $4, $5)
	`, "invite_scope_fixture", scopeOrganizationUUID, "scope@example.com",
		baseInvitedAt, baseInvitedAt.Add(21*24*time.Hour))
	mapper := NewAdminInviteMapper(tx)

	t.Run("failure enforces organization scope", func(t *testing.T) {
		_, findErr := mapper.FindByExternalID(ctx, organizationUUID, "invite_scope_fixture")
		if !errors.Is(findErr, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() error = %v, want sql.ErrNoRows", findErr)
		}
	})

	t.Run("failure does not find a cross-organization page anchor", func(t *testing.T) {
		_, found, findErr := mapper.FindPageAnchorByExternalID(ctx, organizationUUID, "invite_scope_fixture")
		if findErr != nil || found {
			t.Fatalf("FindPageAnchorByExternalID() = (_, %t, %v), want false, nil", found, findErr)
		}
	})

	t.Run("failure does not delete a cross-organization invite", func(t *testing.T) {
		_, deleteErr := mapper.SoftDeleteByExternalID(ctx, organizationUUID, "invite_scope_fixture")
		if !errors.Is(deleteErr, sql.ErrNoRows) {
			t.Fatalf("SoftDeleteByExternalID() error = %v, want sql.ErrNoRows", deleteErr)
		}
	})

	t.Run("success creates lists paginates and soft deletes invites", func(t *testing.T) {
		inviteIDs := []string{"invite_oldest", "invite_middle", "invite_newest"}
		for index, externalID := range inviteIDs {
			invitedAt := baseInvitedAt.Add(time.Duration(index) * time.Second)
			invite, insertErr := mapper.Insert(ctx, insertAdminInviteParams{
				ExternalID:       externalID,
				OrganizationUUID: uuid.MustParse(organizationUUID),
				Email:            externalID + "@example.com",
				Role:             "developer",
				Status:           "pending",
				InvitedAt:        invitedAt,
				ExpiresAt:        invitedAt.Add(21 * 24 * time.Hour),
			})
			if insertErr != nil || invite.ExternalID != externalID {
				t.Fatalf("Insert(%q) = (%+v, %v)", externalID, invite, insertErr)
			}
		}

		invites, listErr := mapper.ListPage(ctx, organizationUUID, nil, false, len(inviteIDs))
		if listErr != nil {
			t.Fatalf("ListPage() error = %v", listErr)
		}
		assertAdminInviteIDs(t, invites, []string{"invite_newest", "invite_middle", "invite_oldest"})

		afterAnchor := findAdminInviteMapperFixtureAnchor(t, ctx, mapper, organizationUUID, "invite_newest")
		invites, listErr = mapper.ListPage(ctx, organizationUUID, &afterAnchor, false, 1)
		if listErr != nil {
			t.Fatalf("ListPage() after cursor error = %v", listErr)
		}
		assertAdminInviteIDs(t, invites, []string{"invite_middle"})

		beforeAnchor := findAdminInviteMapperFixtureAnchor(t, ctx, mapper, organizationUUID, "invite_oldest")
		invites, listErr = mapper.ListPage(ctx, organizationUUID, &beforeAnchor, true, 1)
		if listErr != nil {
			t.Fatalf("ListPage() before cursor error = %v", listErr)
		}
		assertAdminInviteIDs(t, invites, []string{"invite_newest"})

		deleted, deleteErr := mapper.SoftDeleteByExternalID(ctx, organizationUUID, "invite_middle")
		if deleteErr != nil || deleted.Status != "deleted" {
			t.Fatalf("SoftDeleteByExternalID() = (%+v, %v)", deleted, deleteErr)
		}
		foundDeleted, findErr := mapper.FindByExternalID(ctx, organizationUUID, "invite_middle")
		if findErr != nil || foundDeleted.Status != "deleted" {
			t.Fatalf("FindByExternalID() after delete = (%+v, %v)", foundDeleted, findErr)
		}
	})

	for _, expected := range []string{
		`component=database`,
		`statement=AdminInviteMapper.Insert`,
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("mapper logs do not contain %q: %s", expected, logs.String())
		}
	}
}

func findAdminInviteMapperFixtureAnchor(
	t *testing.T,
	ctx context.Context,
	mapper AdminInviteMapper,
	organizationUUID, externalID string,
) pagePosition {
	t.Helper()
	anchor, found, err := mapper.FindPageAnchorByExternalID(ctx, organizationUUID, externalID)
	if err != nil || !found {
		t.Fatalf("FindPageAnchorByExternalID(%q) = (%+v, %t, %v)", externalID, anchor, found, err)
	}
	return anchor
}

func assertAdminInviteIDs(t *testing.T, invites []AdminInvite, wantIDs []string) {
	t.Helper()
	if len(invites) != len(wantIDs) {
		t.Fatalf("invites = %+v, want IDs %v", invites, wantIDs)
	}
	for index, wantID := range wantIDs {
		if invites[index].ExternalID != wantID {
			t.Fatalf("invite[%d] ID = %q, want %q", index, invites[index].ExternalID, wantID)
		}
	}
}

package db

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/google/uuid"
)

func TestAdminUserMapperPostgreSQL(t *testing.T) {
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

	// Keep one database/sql connection alive so public DB methods share the temporary fixture tables.
	database.sql.SetMaxOpenConns(1)
	database.sql.SetMaxIdleConns(1)
	execMapperFixtureSQL(t, ctx, database.mapperDB, `
		CREATE TEMPORARY TABLE users (
			uuid uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			external_id text NOT NULL UNIQUE,
			organization_uuid uuid NOT NULL,
			email text NOT NULL,
			name text NOT NULL,
			role text NOT NULL,
			added_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL DEFAULT NOW(),
			deleted_at timestamptz
		);
		CREATE TEMPORARY TABLE workspace_members (
			organization_uuid uuid NOT NULL,
			user_uuid uuid NOT NULL,
			prevent_delete boolean NOT NULL DEFAULT false,
			updated_at timestamptz NOT NULL DEFAULT NOW(),
			deleted_at timestamptz,
			CHECK (NOT prevent_delete OR deleted_at IS NULL)
		)
	`)

	organizationUUID := "11111111-1111-4111-8111-111111111111"
	otherOrganizationUUID := "22222222-2222-4222-8222-222222222222"
	userUUIDs := []uuid.UUID{
		uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		uuid.MustParse("44444444-4444-4444-8444-444444444444"),
		uuid.MustParse("55555555-5555-4555-8555-555555555555"),
	}
	userIDs := []string{"user_mapper_newest", "user_mapper_middle", "user_mapper_oldest"}
	baseAddedAt := time.Date(2026, time.August, 5, 3, 2, 1, 0, time.UTC)
	for index, userID := range userIDs {
		addedAt := baseAddedAt.Add(-time.Duration(index) * time.Minute)
		execMapperFixtureSQL(t, ctx, database.mapperDB, `
			INSERT INTO users (
				uuid, external_id, organization_uuid, email, name, role, added_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, 'developer', $6, $6)
		`, userUUIDs[index], userID, organizationUUID, userID+"@example.com", "Mapper "+userID, addedAt)
	}
	execMapperFixtureSQL(t, ctx, database.mapperDB, `
		INSERT INTO workspace_members (organization_uuid, user_uuid, prevent_delete)
		VALUES ($1, $2, true), ($1, $3, false)
	`, organizationUUID, userUUIDs[0], userUUIDs[1])

	t.Run("failure enforces organization scope", func(t *testing.T) {
		_, getErr := database.GetAdminUser(ctx, otherOrganizationUUID, userIDs[0])
		if !errors.Is(getErr, ErrNotFound) {
			t.Fatalf("GetAdminUser() error = %v, want ErrNotFound", getErr)
		}
		_, updateErr := database.UpdateAdminUserRole(ctx, otherOrganizationUUID, userIDs[0], "admin")
		if !errors.Is(updateErr, ErrNotFound) {
			t.Fatalf("UpdateAdminUserRole() error = %v, want ErrNotFound", updateErr)
		}
		_, deleteErr := database.DeleteAdminUser(ctx, otherOrganizationUUID, userIDs[0])
		if !errors.Is(deleteErr, ErrNotFound) {
			t.Fatalf("DeleteAdminUser() error = %v, want ErrNotFound", deleteErr)
		}
	})

	t.Run("failure returns an empty page for an unknown cursor", func(t *testing.T) {
		users, hasMore, listErr := database.ListAdminUsersPage(ctx, ListAdminUsersParams{
			OrganizationUUID: organizationUUID,
			AfterID:          "user_mapper_missing",
			Limit:            1,
		})
		if listErr != nil || len(users) != 0 || hasMore {
			t.Fatalf("ListAdminUsersPage() = (%+v, %t, %v), want empty page", users, hasMore, listErr)
		}
	})

	t.Run("failure rolls back the user delete when membership cleanup fails", func(t *testing.T) {
		_, deleteErr := database.DeleteAdminUser(ctx, organizationUUID, userIDs[0])
		if deleteErr == nil {
			t.Fatal("DeleteAdminUser() error = nil, want membership cleanup error")
		}
		user, getErr := database.GetAdminUser(ctx, organizationUUID, userIDs[0])
		if getErr != nil || user.UUID != userUUIDs[0] {
			t.Fatalf("GetAdminUser() after rollback = (%+v, %v)", user, getErr)
		}
	})

	t.Run("success gets filters and paginates users", func(t *testing.T) {
		user, getErr := database.GetAdminUser(ctx, organizationUUID, userIDs[0])
		if getErr != nil || user.UUID != userUUIDs[0] {
			t.Fatalf("GetAdminUser() = (%+v, %v)", user, getErr)
		}

		users, hasMore, listErr := database.ListAdminUsersPage(ctx, ListAdminUsersParams{
			OrganizationUUID: organizationUUID,
			Limit:            1,
		})
		if listErr != nil || len(users) != 1 || users[0].ExternalID != userIDs[0] || !hasMore {
			t.Fatalf("first ListAdminUsersPage() = (%+v, %t, %v)", users, hasMore, listErr)
		}

		users, hasMore, listErr = database.ListAdminUsersPage(ctx, ListAdminUsersParams{
			OrganizationUUID: organizationUUID,
			AfterID:          userIDs[0],
			Limit:            2,
		})
		if listErr != nil || len(users) != 2 || users[0].ExternalID != userIDs[1] || users[1].ExternalID != userIDs[2] || hasMore {
			t.Fatalf("next ListAdminUsersPage() = (%+v, %t, %v)", users, hasMore, listErr)
		}

		users, hasMore, listErr = database.ListAdminUsersPage(ctx, ListAdminUsersParams{
			OrganizationUUID: organizationUUID,
			Email:            strings.ToUpper(userIDs[2] + "@example.com"),
			Limit:            2,
		})
		if listErr != nil || len(users) != 1 || users[0].ExternalID != userIDs[2] || hasMore {
			t.Fatalf("filtered ListAdminUsersPage() = (%+v, %t, %v)", users, hasMore, listErr)
		}
	})

	t.Run("success updates and transactionally deletes a user and memberships", func(t *testing.T) {
		updated, updateErr := database.UpdateAdminUserRole(ctx, organizationUUID, userIDs[1], "admin")
		if updateErr != nil || updated.Role != "admin" {
			t.Fatalf("UpdateAdminUserRole() = (%+v, %v)", updated, updateErr)
		}

		deleted, deleteErr := database.DeleteAdminUser(ctx, organizationUUID, userIDs[1])
		if deleteErr != nil || deleted.UUID != userUUIDs[1] {
			t.Fatalf("DeleteAdminUser() = (%+v, %v)", deleted, deleteErr)
		}
		if _, getErr := database.GetAdminUser(ctx, organizationUUID, userIDs[1]); !errors.Is(getErr, ErrNotFound) {
			t.Fatalf("GetAdminUser() after delete error = %v, want ErrNotFound", getErr)
		}
		var activeMemberships int
		if countErr := database.sql.GetContext(ctx, &activeMemberships, `
			SELECT COUNT(*)
			FROM workspace_members
			WHERE organization_uuid = $1
			AND user_uuid = $2
			AND deleted_at IS NULL
		`, organizationUUID, userUUIDs[1]); countErr != nil || activeMemberships != 0 {
			t.Fatalf("active workspace memberships = %d, error = %v, want 0", activeMemberships, countErr)
		}
	})

	for _, expected := range []string{
		`component=database`,
		`statement=AdminUserMapper.ListPage`,
		`statement=AdminUserMapper.SoftDeleteWorkspaceMembersByUserUUID`,
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("mapper logs do not contain %q: %s", expected, logs.String())
		}
	}
}

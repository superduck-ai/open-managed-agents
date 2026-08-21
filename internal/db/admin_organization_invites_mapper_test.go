package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superduck-ai/yourbatis"
)

func TestAdminInviteMapperInsert(t *testing.T) {
	invitedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	params := insertAdminInviteParams{
		ExternalID:       "invite_mapper",
		OrganizationUUID: "11111111-1111-4111-8111-111111111111",
		Email:            "invitee@example.com",
		Role:             "developer",
		Status:           "pending",
		InvitedAt:        invitedAt,
		ExpiresAt:        invitedAt.Add(21 * 24 * time.Hour),
	}
	wantValues := []any{
		params.ExternalID,
		params.OrganizationUUID,
		params.Email,
		params.Role,
		params.Status,
		params.InvitedAt,
		params.ExpiresAt,
	}

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("insert invite failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewAdminInviteMapper(executor).Insert(context.Background(), params)
		if !errors.Is(err, wantErr) {
			t.Fatalf("Insert() error = %v, want query error", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminInviteMapper.Insert",
			yourbatis.StatementInsert,
			wantValues,
		)
	})

	t.Run("unique violation remains detectable", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			queryErr: &pgconn.PgError{Code: "23505"},
		})
		_, err := NewAdminInviteMapper(executor).Insert(context.Background(), params)
		if !isUniqueViolation(err) {
			t.Fatalf("Insert() error = %v, want detectable unique violation", err)
		}
	})

	t.Run("inserted", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminInviteMapperTestColumns(),
			rows:    [][]driver.Value{adminInviteMapperTestRow(params.ExternalID, params.Email, invitedAt)},
		})
		invite, err := NewAdminInviteMapper(executor).Insert(context.Background(), params)
		if err != nil || invite.ExternalID != params.ExternalID || invite.Email != params.Email {
			t.Fatalf("Insert() = (%+v, %v)", invite, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminInviteMapper.Insert",
			yourbatis.StatementInsert,
			wantValues,
			"INSERT INTO organization_invites",
			"RETURNING",
		)
	})
}

func TestAdminInviteMapperFindByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "invite_mapper"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminInviteMapperTestColumns()})
		invite, err := NewAdminInviteMapper(executor).FindByExternalID(
			context.Background(),
			organizationUUID,
			"invite_mapper",
		)
		if !errors.Is(err, sql.ErrNoRows) || invite.UUID != "" {
			t.Fatalf("FindByExternalID() = (%+v, %v), want zero and sql.ErrNoRows", invite, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminInviteMapper.FindByExternalID",
			yourbatis.StatementSelect,
			wantValues,
			"organization_uuid = $1",
			"external_id = $2",
		)
	})

	t.Run("found", func(t *testing.T) {
		invitedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminInviteMapperTestColumns(),
			rows:    [][]driver.Value{adminInviteMapperTestRow("invite_mapper", "invitee@example.com", invitedAt)},
		})
		invite, err := NewAdminInviteMapper(executor).FindByExternalID(
			context.Background(),
			organizationUUID,
			"invite_mapper",
		)
		if err != nil || invite.ExternalID != "invite_mapper" {
			t.Fatalf("FindByExternalID() = (%+v, %v)", invite, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminInviteMapper.FindByExternalID",
			yourbatis.StatementSelect,
			wantValues,
		)
	})
}

func TestAdminInviteMapperFindPageAnchorByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "invite_anchor"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"created_at", "uuid"}})
		anchor, found, err := NewAdminInviteMapper(executor).FindPageAnchorByExternalID(
			context.Background(),
			organizationUUID,
			"invite_anchor",
		)
		if err != nil || found || anchor.UUID != "" {
			t.Fatalf("FindPageAnchorByExternalID() = (%+v, %t, %v), want zero, false, nil", anchor, found, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminInviteMapper.FindPageAnchorByExternalID",
			yourbatis.StatementSelect,
			wantValues,
		)
	})

	t.Run("found", func(t *testing.T) {
		invitedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		inviteUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"created_at", "uuid"},
			rows:    [][]driver.Value{{invitedAt, inviteUUID.String()}},
		})
		anchor, found, err := NewAdminInviteMapper(executor).FindPageAnchorByExternalID(
			context.Background(),
			organizationUUID,
			"invite_anchor",
		)
		if err != nil || !found || anchor.UUID != inviteUUID.String() || !anchor.CreatedAt.Equal(invitedAt) {
			t.Fatalf("FindPageAnchorByExternalID() = (%+v, %t, %v)", anchor, found, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminInviteMapper.FindPageAnchorByExternalID",
			yourbatis.StatementSelect,
			wantValues,
			"SELECT invited_at AS created_at, uuid",
		)
	})
}

func TestAdminInviteMapperListPage(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	anchor := &pagePosition{
		CreatedAt: time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC),
		UUID:      "22222222-2222-4222-8222-222222222222",
	}

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("list invites failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewAdminInviteMapper(executor).ListPage(context.Background(), organizationUUID, nil, false, 3)
		if !errors.Is(err, wantErr) {
			t.Fatalf("ListPage() error = %v, want query error", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminInviteMapper.ListPage",
			yourbatis.StatementSelect,
			[]any{organizationUUID, 3},
		)
	})

	t.Run("listed without cursor", func(t *testing.T) {
		invitedAt := anchor.CreatedAt
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminInviteMapperTestColumns(),
			rows: [][]driver.Value{
				adminInviteMapperTestRow("invite_newer", "newer@example.com", invitedAt),
				adminInviteMapperTestRow("invite_older", "older@example.com", invitedAt.Add(-time.Minute)),
			},
		})
		invites, err := NewAdminInviteMapper(executor).ListPage(
			context.Background(),
			organizationUUID,
			nil,
			false,
			3,
		)
		if err != nil || len(invites) != 2 || invites[0].ExternalID != "invite_newer" {
			t.Fatalf("ListPage() = (%+v, %v)", invites, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminInviteMapper.ListPage",
			yourbatis.StatementSelect,
			[]any{organizationUUID, 3},
			"ORDER BY invited_at DESC, uuid DESC",
			"LIMIT $2",
		)
	})

	for _, test := range []struct {
		name       string
		before     bool
		comparison string
	}{
		{name: "listed after cursor", comparison: "(invited_at, uuid) < ($2, $3)"},
		{name: "listed before cursor", before: true, comparison: "(invited_at, uuid) > ($2, $3)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminInviteMapperTestColumns()})
			invites, err := NewAdminInviteMapper(executor).ListPage(
				context.Background(),
				organizationUUID,
				anchor,
				test.before,
				3,
			)
			if err != nil || len(invites) != 0 {
				t.Fatalf("ListPage() = (%+v, %v), want empty, nil", invites, err)
			}
			assertMapperTestExecution(
				t,
				executor,
				"AdminInviteMapper.ListPage",
				yourbatis.StatementSelect,
				[]any{organizationUUID, anchor.CreatedAt, anchor.UUID, 3},
				test.comparison,
				"ORDER BY invited_at DESC, uuid DESC",
			)
		})
	}
}

func TestAdminInviteMapperSoftDeleteByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	wantValues := []any{organizationUUID, "invite_mapper"}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: adminInviteMapperTestColumns()})
		invite, err := NewAdminInviteMapper(executor).SoftDeleteByExternalID(
			context.Background(),
			organizationUUID,
			"invite_mapper",
		)
		if !errors.Is(err, sql.ErrNoRows) || invite.UUID != "" {
			t.Fatalf("SoftDeleteByExternalID() = (%+v, %v), want zero and sql.ErrNoRows", invite, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminInviteMapper.SoftDeleteByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
		)
	})

	t.Run("deleted", func(t *testing.T) {
		invitedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		row := adminInviteMapperTestRow("invite_mapper", "invitee@example.com", invitedAt)
		row[5] = "deleted"
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminInviteMapperTestColumns(),
			rows:    [][]driver.Value{row},
		})
		invite, err := NewAdminInviteMapper(executor).SoftDeleteByExternalID(
			context.Background(),
			organizationUUID,
			"invite_mapper",
		)
		if err != nil || invite.Status != "deleted" {
			t.Fatalf("SoftDeleteByExternalID() = (%+v, %v)", invite, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminInviteMapper.SoftDeleteByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
			"SET status = 'deleted'",
			"deleted_at = COALESCE(deleted_at, NOW())",
			"RETURNING",
		)
	})
}

func adminInviteMapperTestColumns() []string {
	return []string{
		"uuid",
		"external_id",
		"organization_uuid",
		"email",
		"role",
		"status",
		"invited_at",
		"expires_at",
	}
}

func adminInviteMapperTestRow(externalID, email string, invitedAt time.Time) []driver.Value {
	return []driver.Value{
		"22222222-2222-4222-8222-222222222222",
		externalID,
		"11111111-1111-4111-8111-111111111111",
		email,
		"developer",
		"pending",
		invitedAt,
		invitedAt.Add(21 * 24 * time.Hour),
	}
}

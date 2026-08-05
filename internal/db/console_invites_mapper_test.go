package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestConsoleInviteMapperList(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	invitedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("list invites failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewConsoleInviteMapper(executor).List(context.Background(), organizationUUID, "pending", 100)
		if !errors.Is(err, wantErr) {
			t.Fatalf("List() error = %v, want query error", err)
		}
	})

	t.Run("listed", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: consoleInviteMapperTestColumns(),
			rows:    [][]driver.Value{consoleInviteMapperTestRow("invite_mapper", "pending", invitedAt)},
		})
		rows, err := NewConsoleInviteMapper(executor).List(context.Background(), organizationUUID, "pending", 100)
		if err != nil || len(rows) != 1 || rows[0].ID != "invite_mapper" {
			t.Fatalf("List() = (%+v, %v)", rows, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"ConsoleInviteMapper.List",
			yourbatis.StatementSelect,
			[]any{organizationUUID, 100},
			"organization_uuid = $1",
			"ORDER BY invited_at DESC, uuid DESC",
			"LIMIT $2",
		)
	})
}

func TestConsoleInviteMapperListBuildsStatusFilters(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	tests := []struct {
		name    string
		status  string
		wantSQL []string
	}{
		{name: "active", wantSQL: []string{"deleted_at IS NULL"}},
		{name: "pending", status: "pending", wantSQL: []string{"status = 'pending'", "expires_at > NOW()"}},
		{name: "expired", status: "expired", wantSQL: []string{"status = 'expired'", "expires_at <= NOW()"}},
		{name: "accepted", status: "accepted", wantSQL: []string{"status = 'accepted'"}},
		{name: "deleted", status: "deleted", wantSQL: []string{"status = 'deleted' OR deleted_at IS NOT NULL"}},
		{name: "unknown", status: "unknown", wantSQL: []string{"AND FALSE"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bound := buildConsoleInviteMapperList(yourbatis.DialectPostgres, organizationUUID, test.status, 100)
			for _, fragment := range test.wantSQL {
				if !strings.Contains(bound.SQL, fragment) {
					t.Fatalf("generated SQL = %q, want fragment %q", bound.SQL, fragment)
				}
			}
			if values := bound.Values(); !reflect.DeepEqual(values, []any{organizationUUID, 100}) {
				t.Fatalf("generated arguments = %#v, want organization and limit", values)
			}
		})
	}
}

func TestConsoleInviteMapperInsert(t *testing.T) {
	invitedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	params := insertConsoleInviteParams{
		ExternalID:       "invite_mapper",
		OrganizationUUID: "11111111-1111-4111-8111-111111111111",
		Email:            "invitee@example.com",
		Role:             "developer",
		InvitedAt:        invitedAt,
		ExpiresAt:        invitedAt.Add(21 * 24 * time.Hour),
	}

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("insert invite failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewConsoleInviteMapper(executor).Insert(context.Background(), params)
		if !errors.Is(err, wantErr) {
			t.Fatalf("Insert() error = %v, want query error", err)
		}
	})

	t.Run("inserted", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: consoleInviteMapperTestColumns(),
			rows:    [][]driver.Value{consoleInviteMapperTestRow(params.ExternalID, "pending", invitedAt)},
		})
		row, err := NewConsoleInviteMapper(executor).Insert(context.Background(), params)
		if err != nil || row.ID != params.ExternalID || row.Status != "pending" {
			t.Fatalf("Insert() = (%+v, %v)", row, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"ConsoleInviteMapper.Insert",
			yourbatis.StatementInsert,
			[]any{
				params.ExternalID,
				params.OrganizationUUID,
				params.Email,
				params.Role,
				params.InvitedAt,
				params.ExpiresAt,
			},
			"INSERT INTO organization_invites",
			"RETURNING",
		)
	})
}

func TestConsoleInviteMapperResendByExternalID(t *testing.T) {
	invitedAt := time.Date(2026, time.August, 5, 2, 3, 4, 0, time.UTC)
	params := resendConsoleInviteParams{
		OrganizationUUID: "11111111-1111-4111-8111-111111111111",
		ExternalID:       "invite_mapper",
		InvitedAt:        invitedAt,
		ExpiresAt:        invitedAt.Add(21 * 24 * time.Hour),
	}
	wantValues := []any{params.InvitedAt, params.ExpiresAt, params.OrganizationUUID, params.ExternalID}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: consoleInviteMapperTestColumns()})
		_, err := NewConsoleInviteMapper(executor).ResendByExternalID(context.Background(), params)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("ResendByExternalID() error = %v, want sql.ErrNoRows", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"ConsoleInviteMapper.ResendByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
			"deleted_at IS NULL",
		)
	})

	t.Run("resent", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: consoleInviteMapperTestColumns(),
			rows:    [][]driver.Value{consoleInviteMapperTestRow(params.ExternalID, "pending", invitedAt)},
		})
		row, err := NewConsoleInviteMapper(executor).ResendByExternalID(context.Background(), params)
		if err != nil || row.Status != "pending" || !row.InvitedAt.Equal(invitedAt) {
			t.Fatalf("ResendByExternalID() = (%+v, %v)", row, err)
		}
	})
}

func TestConsoleInviteMapperSoftDeleteByExternalID(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	externalID := "invite_mapper"
	wantValues := []any{organizationUUID, externalID}

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: consoleInviteMapperTestColumns()})
		_, err := NewConsoleInviteMapper(executor).SoftDeleteByExternalID(
			context.Background(),
			organizationUUID,
			externalID,
		)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("SoftDeleteByExternalID() error = %v, want sql.ErrNoRows", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"ConsoleInviteMapper.SoftDeleteByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
		)
	})

	t.Run("deleted", func(t *testing.T) {
		invitedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: consoleInviteMapperTestColumns(),
			rows:    [][]driver.Value{consoleInviteMapperTestRow(externalID, "deleted", invitedAt)},
		})
		row, err := NewConsoleInviteMapper(executor).SoftDeleteByExternalID(
			context.Background(),
			organizationUUID,
			externalID,
		)
		if err != nil || row.Status != "deleted" {
			t.Fatalf("SoftDeleteByExternalID() = (%+v, %v)", row, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"ConsoleInviteMapper.SoftDeleteByExternalID",
			yourbatis.StatementUpdate,
			wantValues,
			"COALESCE(deleted_at, NOW())",
			"RETURNING",
		)
	})
}

func consoleInviteMapperTestColumns() []string {
	return []string{"id", "email", "role", "status", "invited_at", "expires_at"}
}

func consoleInviteMapperTestRow(externalID, status string, invitedAt time.Time) []driver.Value {
	return []driver.Value{
		externalID,
		"invitee@example.com",
		"developer",
		status,
		invitedAt,
		invitedAt.Add(21 * 24 * time.Hour),
	}
}

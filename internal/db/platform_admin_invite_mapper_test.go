package db

import (
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestPlatformAdminInviteWriteBindings(t *testing.T) {
	org, invitation := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	for _, tc := range []struct {
		name     string
		kind     yourbatis.StatementKind
		call     func(PlatformAdminInviteMapper) error
		fragment string
	}{
		{"Resend", yourbatis.StatementUpdate, func(m PlatformAdminInviteMapper) error { return m.Resend(t.Context(), org, invitation) }, "expires_at = now() + interval '21 days'"},
		{"Revoke", yourbatis.StatementUpdate, func(m PlatformAdminInviteMapper) error { return m.Revoke(t.Context(), org, invitation) }, "status = 'deleted'"},
		{"CancelDeliveries", yourbatis.StatementUpdate, func(m PlatformAdminInviteMapper) error { return m.CancelDeliveries(t.Context(), org, invitation) }, "status IN ('pending', 'sending', 'failed')"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := errors.New("执行失败")
			executor := newMapperTestExecutor(t, mapperTestResponse{execErr: failure})
			if err := tc.call(NewPlatformAdminInviteMapper(executor)); !errors.Is(err, failure) {
				t.Fatalf("未保留执行错误：%v", err)
			}
			executor = newMapperTestExecutor(t, mapperTestResponse{})
			if err := tc.call(NewPlatformAdminInviteMapper(executor)); err != nil {
				t.Fatal(err)
			}
			assertMapperTestExecution(t, executor, "PlatformAdminInviteMapper."+tc.name, tc.kind, []any{org, invitation}, "organization_uuid = $1", tc.fragment)
		})
	}
}

func TestPlatformAdminInvitePaginationBindings(t *testing.T) {
	for _, filters := range []struct{ search, cursor bool }{{}, {true, false}, {false, true}, {true, true}} {
		query := PlatformAdminPageQuery{OrganizationUUID: "11111111-1111-4111-8111-111111111111", Limit: 21}
		args := []any{query.OrganizationUUID}
		if filters.search {
			query.Query = "alice"
			args = append(args, query.Query)
		}
		if filters.cursor {
			query.After = "22222222-2222-4222-8222-222222222222"
			args = append(args, query.After)
		}
		args = append(args, query.Limit)
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "external_id", "email", "role", "status", "invited_at", "expires_at", "delivery_status"}})
		if _, err := NewPlatformAdminInviteMapper(executor).List(t.Context(), query); err != nil {
			t.Fatal(err)
		}
		fragments := []string{"i.organization_uuid = $1", "ORDER BY i.uuid LIMIT"}
		if filters.search {
			fragments = append(fragments, "i.email ILIKE '%' || $2 || '%'")
		}
		if filters.cursor {
			fragment := "i.uuid > $2"
			if filters.search {
				fragment = "i.uuid > $3"
			}
			fragments = append(fragments, fragment)
		}
		assertMapperTestExecution(t, executor, "PlatformAdminInviteMapper.List", yourbatis.StatementSelect, args, fragments...)
	}
}

func TestPlatformAdminInviteReadAndInsertBindings(t *testing.T) {
	input := PlatformAdminInviteInput{UUID: "invite", ExternalID: "invite_external", OrganizationUUID: "org", AccountUUID: "actor", Email: "invitee@example.com", Role: "user"}
	now := time.Now()
	invitation := mapperTestResponse{columns: []string{"uuid", "external_id", "email", "role", "status", "invited_at", "expires_at", "delivery_status"}, rows: [][]driver.Value{{input.UUID, input.ExternalID, input.Email, input.Role, "pending", now, now.Add(21 * 24 * time.Hour), "pending"}}}
	for _, tc := range []struct {
		name      string
		call      func(PlatformAdminInviteMapper) error
		response  mapperTestResponse
		kind      yourbatis.StatementKind
		args      []any
		fragments []string
	}{
		{"Lock", func(m PlatformAdminInviteMapper) error {
			_, err := m.Lock(t.Context(), input.OrganizationUUID, input.UUID)
			return err
		}, invitation, yourbatis.StatementSelect, []any{input.OrganizationUUID, input.UUID}, []string{"i.organization_uuid = $1 AND i.uuid = $2", "FOR UPDATE OF i", "d.organization_uuid = i.organization_uuid AND d.invitation_uuid = i.uuid", "ORDER BY d.created_at DESC, d.uuid DESC LIMIT 1"}},
		{"Insert", func(m PlatformAdminInviteMapper) error { _, err := m.Insert(t.Context(), input); return err }, invitation, yourbatis.StatementInsert, []any{input.UUID, input.ExternalID, input.OrganizationUUID, input.Email, input.Role, input.AccountUUID}, []string{"VALUES ($1, $2, $3, $4, $5, 'pending', now(), now() + interval '21 days', $6)", "RETURNING uuid, external_id, email, role, status, invited_at, expires_at, 'pending' AS delivery_status"}},
		{"HasPending", func(m PlatformAdminInviteMapper) error {
			_, err := m.HasPending(t.Context(), input.OrganizationUUID, input.Email)
			return err
		}, mapperTestResponse{columns: []string{"pending"}, rows: [][]driver.Value{{true}}}, yourbatis.StatementSelect, []any{input.OrganizationUUID, input.Email}, []string{"organization_uuid = $1", "lower(email) = lower($2) AND status = 'pending' AND deleted_at IS NULL AND expires_at > now()"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := errors.New("查询失败")
			executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: failure})
			if err := tc.call(NewPlatformAdminInviteMapper(executor)); !errors.Is(err, failure) {
				t.Fatalf("未保留查询错误：%v", err)
			}
			executor = newMapperTestExecutor(t, tc.response)
			if err := tc.call(NewPlatformAdminInviteMapper(executor)); err != nil {
				t.Fatal(err)
			}
			assertMapperTestExecution(t, executor, "PlatformAdminInviteMapper."+tc.name, tc.kind, tc.args, tc.fragments...)
		})
	}
}

func TestPlatformAdminEnqueueBindings(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}, rows: [][]driver.Value{{"delivery"}}})
	id, err := NewPlatformAdminInviteMapper(executor).Enqueue(t.Context(), "org", "invitation")
	if err != nil || id != "delivery" {
		t.Fatalf("投递记录：%s %v", id, err)
	}
	assertMapperTestExecution(t, executor, "PlatformAdminInviteMapper.Enqueue", yourbatis.StatementInsert, []any{"org", "invitation"}, "organization_uuid = $1", "uuid = $2", "RETURNING uuid")
}

package db

import (
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestInvitationMapperBindings(t *testing.T) {
	org, email, id := "11111111-1111-4111-8111-111111111111", "User@example.com", "invite_test"
	columns := []string{"id", "organization_uuid", "organization_name", "email", "role", "status", "invited_at", "expires_at"}
	t.Run("查询失败透传", func(t *testing.T) {
		want := errors.New("query failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: want})
		_, _, err := NewInvitationMapper(executor).LockByIDAndEmail(t.Context(), id, email)
		if !errors.Is(err, want) {
			t.Fatalf("错误 = %v", err)
		}
	})
	t.Run("无匹配邀请", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: columns})
		_, found, err := NewInvitationMapper(executor).LockByIDAndEmail(t.Context(), id, email)
		if err != nil || found {
			t.Fatalf("found=%v err=%v", found, err)
		}
		assertMapperTestExecution(t, executor, "InvitationMapper.LockByIDAndEmail", yourbatis.StatementSelect, []any{id, email}, "i.external_id = $1", "lower(i.email) = lower($2)", "FOR UPDATE OF i")
	})
	t.Run("扫描失败", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: columns, rows: [][]driver.Value{{id, org, "组织", email, "user", "pending", "invalid", time.Now()}}})
		if _, _, err := NewInvitationMapper(executor).LockByIDAndEmail(t.Context(), id, email); err == nil {
			t.Fatal("应拒绝无效时间")
		}
	})
	t.Run("列表", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: columns})
		if _, err := NewInvitationMapper(executor).ListByEmail(t.Context(), email); err != nil {
			t.Fatal(err)
		}
		assertMapperTestExecution(t, executor, "InvitationMapper.ListByEmail", yourbatis.StatementSelect, []any{email}, "i.status = 'pending'", "i.expires_at > clock_timestamp()", "ORDER BY i.invited_at DESC, i.uuid DESC")
	})
	t.Run("成员查询", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"id", "role"}, rows: [][]driver.Value{{"user_test", "admin"}}})
		member, found, err := NewInvitationMapper(executor).FindActiveMember(t.Context(), org, email)
		if err != nil || !found || member.Role != "admin" {
			t.Fatalf("成员=%+v found=%v err=%v", member, found, err)
		}
		assertMapperTestExecution(t, executor, "InvitationMapper.FindActiveMember", yourbatis.StatementSelect, []any{org, email}, "organization_uuid = $1", "deleted_at IS NULL FOR UPDATE")
	})
	t.Run("成员插入冲突", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{})
		count, err := NewInvitationMapper(executor).InsertMember(t.Context(), invitationMemberParams{ID: "user_test", OrganizationUUID: org, Email: email, Role: "developer"})
		if err != nil || count != 0 {
			t.Fatalf("count=%d err=%v", count, err)
		}
		assertMapperTestExecution(t, executor, "InvitationMapper.InsertMember", yourbatis.StatementInsert, []any{"user_test", org, email, "developer"}, "ON CONFLICT (organization_uuid, lower(email)) WHERE deleted_at IS NULL DO NOTHING")
	})
	t.Run("状态转换绑定", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{})
		if _, err := NewInvitationMapper(executor).UpdateStatus(t.Context(), org, id, "declined"); err != nil {
			t.Fatal(err)
		}
		assertMapperTestExecution(t, executor, "InvitationMapper.UpdateStatus", yourbatis.StatementUpdate, []any{"declined", org, id}, "organization_uuid = $2 AND external_id = $3", "status = 'pending'", "expires_at > clock_timestamp()")
	})
}

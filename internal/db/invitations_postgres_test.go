package db

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestInvitationsPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("需要隔离 PostgreSQL 测试地址")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 61); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	org := "61000000-0000-0000-0000-000000000001"
	execMapperFixtureSQL(t, ctx, store.mapperDB, `INSERT INTO organizations (uuid,name) VALUES ($1,'邀请测试组织')`, org)
	seed := func(id, email, status, role string, expires time.Time) {
		t.Helper()
		execMapperFixtureSQL(t, ctx, store.mapperDB, `INSERT INTO organization_invites (external_id,organization_uuid,email,role,status,expires_at) VALUES ($1,$2,$3,$4,$5,$6)`, id, org, email, role, status, expires)
	}
	future := time.Now().Add(time.Hour)
	t.Run("错误邮箱和终态拒绝", func(t *testing.T) {
		seed("invite_private", "private@example.com", "pending", "user", future)
		if _, err := store.RespondToInvitation(ctx, "invite_private", "other@example.com", true); !errors.Is(err, ErrNotFound) {
			t.Fatalf("错误邮箱 err=%v", err)
		}
		for _, status := range []string{"deleted", "expired", "declined"} {
			id := "invite_terminal_" + status
			seed(id, "terminal@example.com", status, "user", future)
			want := ErrInvitationConflict
			if status == "deleted" {
				want = ErrInvitationRevoked
			}
			if status == "expired" {
				want = ErrInvitationExpired
			}
			if _, err := store.RespondToInvitation(ctx, id, "terminal@example.com", true); !errors.Is(err, want) {
				t.Fatalf("%s err=%v", status, err)
			}
		}
		seed("invite_expired", "expired@example.com", "pending", "user", time.Now().Add(-time.Second))
		for range 2 {
			if _, err := store.RespondToInvitation(ctx, "invite_expired", "expired@example.com", false); !errors.Is(err, ErrInvitationExpired) {
				t.Fatalf("过期 err=%v", err)
			}
		}
	})
	t.Run("接受后移除不能重建", func(t *testing.T) {
		seed("invite_removed", "removed@example.com", "pending", "developer", future)
		if _, err := store.RespondToInvitation(ctx, "invite_removed", "removed@example.com", true); err != nil {
			t.Fatal(err)
		}
		execMapperFixtureSQL(t, ctx, store.mapperDB, `UPDATE users SET deleted_at=NOW() WHERE organization_uuid=$1 AND email=$2`, org, "removed@example.com")
		if _, err := store.RespondToInvitation(ctx, "invite_removed", "removed@example.com", true); !errors.Is(err, ErrInvitationConflict) {
			t.Fatalf("重试 err=%v", err)
		}
		if _, found, err := NewInvitationMapper(store.mapperDB).FindActiveMember(ctx, org, "removed@example.com"); err != nil || found {
			t.Fatalf("重建了成员 found=%v err=%v", found, err)
		}
	})
	t.Run("成员插入失败回滚邀请", func(t *testing.T) {
		seed("invite_rollback", "rollback@example.com", "pending", "developer", future)
		execMapperFixtureSQL(t, ctx, store.mapperDB, `ALTER TABLE users ADD CONSTRAINT invitation_test_reject CHECK (email != 'rollback@example.com')`)
		if _, err := store.RespondToInvitation(ctx, "invite_rollback", "rollback@example.com", true); err == nil {
			t.Fatal("应拒绝插入")
		}
		execMapperFixtureSQL(t, ctx, store.mapperDB, `ALTER TABLE users DROP CONSTRAINT invitation_test_reject`)
		row, _, err := NewInvitationMapper(store.mapperDB).LockByIDAndEmail(ctx, "invite_rollback", "rollback@example.com")
		if err != nil || row.Status != "pending" {
			t.Fatalf("事务未回滚 row=%+v err=%v", row, err)
		}
	})
	t.Run("接受复用已有成员及角色", func(t *testing.T) {
		seed("invite_existing", "existing@example.com", "pending", "user", future)
		execMapperFixtureSQL(t, ctx, store.mapperDB, `INSERT INTO users (external_id,organization_uuid,email,role) VALUES ('user_existing',$1,'existing@example.com','admin')`, org)
		for range 2 {
			row, err := store.RespondToInvitation(ctx, "invite_existing", "EXISTING@example.com", true)
			if err != nil || row.UserID != "user_existing" || row.Role != "admin" || row.Status != "accepted" {
				t.Fatalf("结果=%+v err=%v", row, err)
			}
		}
	})
	t.Run("拒绝幂等且管理员不可复活终态", func(t *testing.T) {
		seed("invite_decline", "decline@example.com", "pending", "user", future)
		for range 2 {
			row, err := store.RespondToInvitation(ctx, "invite_decline", "decline@example.com", false)
			if err != nil || row.Status != "declined" {
				t.Fatalf("结果=%+v err=%v", row, err)
			}
		}
		for _, id := range []string{"invite_decline", "invite_existing"} {
			if _, err := store.ResendConsoleInvite(ctx, org, id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("重发 %s err=%v", id, err)
			}
			if _, err := store.DeleteConsoleInvite(ctx, org, id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("撤销 %s err=%v", id, err)
			}
			if _, err := store.DeleteAdminInvite(ctx, org, id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("管理员撤销 %s err=%v", id, err)
			}
		}
	})
	t.Run("不同邀请并发接受同组织邮箱", func(t *testing.T) {
		ids := []string{"invite_concurrent_a", "invite_concurrent_b"}
		for _, id := range ids {
			seed(id, "concurrent@example.com", "pending", "developer", future)
		}
		start := make(chan struct{})
		results := make(chan Invitation, 12)
		errs := make(chan error, 12)
		var group sync.WaitGroup
		for i := range 12 {
			group.Go(func() {
				<-start
				row, err := store.RespondToInvitation(ctx, ids[i%2], "concurrent@example.com", true)
				results <- row
				errs <- err
			})
		}
		close(start)
		group.Wait()
		close(results)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		userID := ""
		for row := range results {
			if row.UserID == "" || row.Status != "accepted" {
				t.Fatalf("结果=%+v", row)
			}
			if userID != "" && row.UserID != userID {
				t.Fatal("并发创建了多个成员")
			}
			userID = row.UserID
		}
	})
	t.Run("接受拒绝并发仅一个成功", func(t *testing.T) {
		seed("invite_race", "race@example.com", "pending", "user", future)
		start := make(chan struct{})
		errs := make(chan error, 2)
		var group sync.WaitGroup
		for _, accept := range []bool{true, false} {
			group.Go(func() {
				<-start
				_, err := store.RespondToInvitation(ctx, "invite_race", "race@example.com", accept)
				errs <- err
			})
		}
		close(start)
		group.Wait()
		close(errs)
		success, conflict := 0, 0
		for err := range errs {
			if err == nil {
				success++
			} else if errors.Is(err, ErrInvitationConflict) {
				conflict++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("success=%d conflict=%d", success, conflict)
		}
	})
	t.Run("列表仅有效邀请且未创建工作区成员或密钥", func(t *testing.T) {
		seed("invite_visible", "list@example.com", "pending", "user", future)
		seed("invite_hidden", "list@example.com", "declined", "user", future)
		rows, err := store.ListInvitations(ctx, "LIST@example.com")
		if err != nil || len(rows) != 1 || rows[0].ID != "invite_visible" || rows[0].OrganizationUUID != org {
			t.Fatalf("列表=%+v err=%v", rows, err)
		}
		var count int
		if err := database.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM workspace_members)+(SELECT count(*) FROM api_keys)`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("额外权限 count=%d err=%v", count, err)
		}
	})
}

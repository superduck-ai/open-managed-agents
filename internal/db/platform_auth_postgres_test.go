package db

import (
	"errors"
	"os"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/yourbatis"
)

func TestPlatformLoginIdentityPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("需要隔离 PostgreSQL 测试地址")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 62); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	home := "62000000-0000-4000-8000-000000000001"
	other := "62000000-0000-4000-8000-000000000002"
	original := "62000000-0000-4000-8000-000000000003"
	member := "62000000-0000-4000-8000-000000000004"
	execMapperFixtureSQL(t, ctx, store.mapperDB, `
		INSERT INTO organizations (uuid, name) VALUES ($1, '任意名称'), ($2, '任意名称')
	`, home, other)
	execMapperFixtureSQL(t, ctx, store.mapperDB, `
		INSERT INTO users (uuid, external_id, organization_uuid, email, role, registration_identity, deleted_at, added_at)
		VALUES ($1, 'user_login', $2, 'Login@Example.com', 'user', true, NOW(), NOW()),
		       ($3, 'user_member', $4, 'login@example.com', 'admin', false, NULL, NOW() - INTERVAL '1 day')
	`, original, home, member, other)

	t.Run("软删身份不能用于组织授权", func(t *testing.T) {
		if _, err := store.GetActivePlatformUserByEmail(ctx, home, "login@example.com"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("软删成员查询: %v", err)
		}
		if _, err := store.EnrichPlatformSession(ctx, platformsession.Session{OrganizationUUID: other, UserUUID: original}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("跨组织身份查询: %v", err)
		}
	})
	t.Run("注册来源优先于更早的成员记录", func(t *testing.T) {
		row, err := NewPlatformAuthUserMapper(store.mapperDB).FindContextByEmail(ctx, "LOGIN@example.com")
		if err != nil || row.UserExternalID != "user_login" || row.OrgUUID != home {
			t.Fatalf("登录身份: %+v, %v", row, err)
		}
	})
	t.Run("无工作区的历史会话补齐身份但不改变权限", func(t *testing.T) {
		session, err := store.EnrichPlatformSession(ctx, platformsession.Session{
			ExternalID: "session_original", OrganizationUUID: home, UserExternalID: "user_login",
		})
		if err != nil || session.UserUUID != original || session.VerifiedEmail != "login@example.com" ||
			session.HomeOrganizationUUID != home || session.WorkspaceUUID != "" || session.ExternalID != "session_original" {
			t.Fatalf("历史会话: %+v, %v", session, err)
		}
		created, err := store.ResolvePlatformSessionIdentity(ctx, platformsession.CreateInput{
			SessionKey: "trusted-key", UserUUID: original, OrgUUID: home,
		})
		if err != nil || created.UserUUID != original || created.VerifiedEmail != session.VerifiedEmail {
			t.Fatalf("软删身份重新登录: %+v, %v", created, err)
		}
	})
	t.Run("邮箱匹配返回目标组织用户与所有有效成员", func(t *testing.T) {
		user, err := store.GetActivePlatformUserByEmail(ctx, other, "LOGIN@example.com")
		if err != nil || user.UUID != member || user.ExternalID != "user_member" {
			t.Fatalf("目标用户: %+v, %v", user, err)
		}
		orgs, err := store.ListBootstrapOrganizationsByEmail(ctx, "LOGIN@example.com")
		if err != nil || len(orgs) != 1 || orgs[0].UUID != other || orgs[0].UserUUID != member || orgs[0].UserExternalID != "user_member" {
			t.Fatalf("组织成员: %+v, %v", orgs, err)
		}
		userRecord, err := store.GetBootstrapUser(ctx, original)
		if err != nil || userRecord.UUID != original {
			t.Fatalf("稳定账号: %+v, %v", userRecord, err)
		}
	})
	t.Run("旧身份仅回退到登录来源组织", func(t *testing.T) {
		session, err := store.EnrichPlatformSession(ctx, platformsession.Session{OrganizationUUID: other, UserUUID: member})
		if err != nil || session.HomeOrganizationUUID != other {
			t.Fatalf("旧身份来源: %+v, %v", session, err)
		}
	})
}

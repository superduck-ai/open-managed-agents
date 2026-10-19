package db

import (
	"errors"
	"github.com/superduck-ai/yourbatis"
	"os"
	"testing"
	"uuid"
)

func TestPlatformAdminHistoricalIdentityPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL，跳过不代表验收通过")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	home, other := uuid.NewV4().String(), uuid.NewV4().String()
	execMapperFixtureSQL(t, ctx, store.mapperDB, `INSERT INTO organizations (uuid,name) VALUES ($1,'历史来源'),($2,'成员组织')`, home, other)
	seed, err := NewAdminUserMapper(store.mapperDB).SeedDefault(ctx, "seed_identity", home, "Legacy@Example.com", "历史账号")
	if err != nil {
		t.Fatal(err)
	}
	member := uuid.NewV4().String()
	execMapperFixtureSQL(t, ctx, store.mapperDB, `INSERT INTO users (uuid,external_id,organization_uuid,email,role,added_at) VALUES ($1,'legacy_member',$2,'legacy@example.com','admin',now()+interval '1 day')`, member, other)
	t.Run("拒绝同邮箱的非登录成员身份", func(t *testing.T) {
		if _, err := store.GetPlatformAdminAccount(ctx, member); !errors.Is(err, ErrNotFound) {
			t.Fatalf("成员冒充登录身份：%v", err)
		}
	})
	t.Run("seed历史账号只读兼容并需显式授权", func(t *testing.T) {
		account, err := store.GetPlatformAdminAccount(ctx, seed)
		if err != nil || account.Granted {
			t.Fatalf("历史账号查询或自动授权：%+v %v", account, err)
		}
		login, err := NewPlatformAuthUserMapper(store.mapperDB).FindContextByEmail(ctx, "LEGACY@example.com")
		if err != nil || login.UserExternalID != "seed_identity" {
			t.Fatalf("后台与登录解析不一致：%+v %v", login, err)
		}
		rows, err := store.ListPlatformAdminAccounts(ctx, PlatformAdminPageQuery{Query: "legacy@", Limit: 20})
		if err != nil || len(rows) != 1 || rows[0].UUID != seed {
			t.Fatalf("账号选择器：%+v %v", rows, err)
		}
		err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			identity, err := tx.Account(ctx, seed)
			if err != nil {
				return err
			}
			return tx.SetGrant(ctx, PlatformAdminGrant{AccountUUID: identity.UUID, Operator: "明确授权的测试运维"}, true)
		})
		if err != nil {
			t.Fatal(err)
		}
		account, err = store.GetPlatformAdminAccount(ctx, seed)
		if err != nil || !account.Granted {
			t.Fatalf("历史身份授权失败：%+v %v", account, err)
		}
		var marked int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE registration_identity`).Scan(&marked); err != nil || marked != 0 {
			t.Fatalf("发生了注册标记回填：%d %v", marked, err)
		}
		var unchanged string
		if err := database.QueryRowContext(ctx, `SELECT organization_uuid FROM users WHERE uuid=$1`, seed).Scan(&unchanged); err != nil || unchanged != home {
			t.Fatalf("来源组织被修改：%s %v", unchanged, err)
		}
	})
	t.Run("明确注册来源优先于更早历史成员", func(t *testing.T) {
		registered := uuid.NewV4().String()
		registeredHome := uuid.NewV4().String()
		execMapperFixtureSQL(t, ctx, store.mapperDB, `INSERT INTO organizations (uuid,name) VALUES ($1,'明确注册来源')`, registeredHome)
		execMapperFixtureSQL(t, ctx, store.mapperDB, `INSERT INTO users (uuid,external_id,organization_uuid,email,role,registration_identity,added_at) VALUES ($1,'registered_identity',$2,'legacy@example.com','user',true,now()+interval '2 days')`, registered, registeredHome)
		if _, err := store.GetPlatformAdminAccount(ctx, seed); !errors.Is(err, ErrNotFound) {
			t.Fatalf("未优先采用注册来源：%v", err)
		}
		account, err := store.GetPlatformAdminAccount(ctx, registered)
		if err != nil || account.Granted {
			t.Fatalf("新身份被自动授权：%+v %v", account, err)
		}
		login, err := NewPlatformAuthUserMapper(store.mapperDB).FindContextByEmail(ctx, "legacy@example.com")
		if err != nil || login.UserExternalID != "registered_identity" {
			t.Fatalf("登录选择不一致：%+v %v", login, err)
		}
	})
}

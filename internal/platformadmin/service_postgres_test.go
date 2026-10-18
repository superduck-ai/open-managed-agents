package platformadmin

import (
	"context"
	"database/sql"
	"errors"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func administrationDatabase(t *testing.T) *db.DB {
	t.Helper()
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL；跳过不能视为验收通过")
	}
	ctx := t.Context()
	admin, err := pgx.Connect(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	schema := "pa_test_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = admin.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(context.Background())
	})
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := db.Open(ctx, config.Config{Database: config.DatabaseConfig{URL: parsed.String()}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestAdministrationServicePostgres(t *testing.T) {
	database := administrationDatabase(t)
	service := administrationService(t, database)
	ctx := t.Context()
	a, b := uuid.NewV4().String(), uuid.NewV4().String()
	err := database.WithPlatformAuthTx(ctx, func(tx db.PlatformAuthTxStore) error {
		org, err := tx.InsertOrganization(ctx, db.PlatformAuthOrganizationInput{Name: "注册来源"})
		if err != nil {
			return err
		}
		for _, account := range []string{a, b} {
			if _, err := tx.InsertUser(ctx, db.PlatformAuthUserInput{UUID: account, ExternalID: "user_" + account, OrganizationUUID: org.UUID, Email: account + "@example.com", Role: "admin"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{a, b} {
		if err := service.SetAdministrator(ctx, account, "测试运维", "初始化", true); err != nil {
			t.Fatal(err)
		}
	}
	actor := Actor{AccountUUID: a}
	t.Run("无授权创建被拒绝", func(t *testing.T) {
		_, _, err := service.CreateOrganization(ctx, Actor{AccountUUID: uuid.NewV4().String()}, CreateOrganization{Name: "拒绝创建", Reason: "测试", AdminAccountUUIDs: []string{a}}, "denied")
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("错误 = %v", err)
		}
	})
	t.Run("无效管理员创建原子回滚", func(t *testing.T) {
		before, err := database.ListPlatformAdminOrganizations(ctx, db.PlatformAdminPageQuery{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = service.CreateOrganization(ctx, actor, CreateOrganization{Name: "回滚", Reason: "测试", AdminAccountUUIDs: []string{uuid.NewV4().String()}}, "rollback")
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("错误 = %v", err)
		}
		after, err := database.ListPlatformAdminOrganizations(ctx, db.PlatformAdminPageQuery{Limit: 100})
		if err != nil || len(before) != len(after) {
			t.Fatalf("留下空组织：%v", err)
		}
	})
	input := CreateOrganization{Name: "受管组织", Reason: "验收", AdminAccountUUIDs: []string{a}}
	org, _, err := service.CreateOrganization(ctx, actor, input, "create")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("幂等重放与参数冲突", func(t *testing.T) {
		replayed, hit, err := service.CreateOrganization(ctx, actor, input, "create")
		if err != nil || !hit || replayed.UUID != org.UUID {
			t.Fatalf("幂等重放：%+v，%v，%v", replayed, hit, err)
		}
		changed := input
		changed.Name = "不同组织"
		if _, _, err := service.CreateOrganization(ctx, actor, changed, "create"); !errors.Is(err, ErrIdempotency) {
			t.Fatalf("幂等冲突：%v", err)
		}
	})
	t.Run("幂等重放返回修改后的组织资料", func(t *testing.T) {
		updated, err := service.UpdateOrganization(ctx, actor, org.UUID, OrganizationPatch{Change: Change{ExpectedVersion: org.Version}, Name: new("更新后的组织"), Website: new("https://example.com")})
		if err != nil {
			t.Fatal(err)
		}
		replayed, hit, err := service.CreateOrganization(ctx, actor, input, "create")
		if err != nil || !hit || replayed != updated {
			t.Fatalf("未返回当前资料：%+v %+v %v %v", replayed, updated, hit, err)
		}
		org = updated
	})
	t.Run("失败请求不占用键且不同账号同键独立", func(t *testing.T) {
		first, hit, err := service.CreateOrganization(ctx, actor, input, "rollback")
		if err != nil || hit {
			t.Fatalf("回滚占用了键：%+v %v %v", first, hit, err)
		}
		second, hit, err := service.CreateOrganization(ctx, Actor{AccountUUID: b}, input, "rollback")
		if err != nil || hit || second.UUID == first.UUID {
			t.Fatalf("跨账号键冲突：%+v %v %v", second, hit, err)
		}
	})
	t.Run("并发同键不同参数返回冲突", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, name := range []string{"并发甲", "并发乙"} {
			wg.Go(func() {
				<-start
				_, _, err := service.CreateOrganization(ctx, actor, CreateOrganization{Name: name, AdminAccountUUIDs: []string{a}}, "conflict")
				results <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)
		success, conflicts := 0, 0
		for err := range results {
			switch {
			case err == nil:
				success++
			case errors.Is(err, ErrIdempotency):
				conflicts++
			default:
				t.Fatal(err)
			}
		}
		if success != 1 || conflicts != 1 {
			t.Fatalf("并发冲突：成功 %d，冲突 %d", success, conflicts)
		}
	})
	members, err := database.ListPlatformAdminMembers(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org.UUID, Limit: 10})
	if err != nil || len(members) != 1 {
		t.Fatalf("初始管理员：%+v，%v", members, err)
	}
	first := members[0].UUID
	t.Run("最后管理员不能降级", func(t *testing.T) {
		_, err := service.DemoteAdministrator(ctx, actor, org.UUID, first, AdministratorChange{Change: Change{ExpectedVersion: org.Version, Reason: "测试"}, Role: "user"})
		if !errors.Is(err, ErrLastOrganizationAdmin) {
			t.Fatalf("错误 = %v", err)
		}
	})
	t.Run("交接失败回滚新管理员", func(t *testing.T) {
		_, err := service.TransferAdministrator(ctx, actor, org.UUID, AdministratorChange{Change: Change{ExpectedVersion: org.Version, Reason: "测试"}, AccountUUID: b, PreviousMemberUUID: uuid.NewV4().String(), Role: "user"})
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("错误 = %v", err)
		}
		rows, err := database.ListPlatformAdminMembers(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org.UUID, Limit: 10})
		if err != nil || len(rows) != 1 {
			t.Fatalf("交接未回滚：%+v，%v", rows, err)
		}
	})
	t.Run("原子交接并拒绝旧版本", func(t *testing.T) {
		updated, err := service.TransferAdministrator(ctx, actor, org.UUID, AdministratorChange{Change: Change{ExpectedVersion: org.Version, Reason: "交接"}, AccountUUID: b, PreviousMemberUUID: first, Role: "user"})
		if err != nil || updated.AdminCount != 1 || updated.MemberCount != 2 {
			t.Fatalf("交接：%+v，%v", updated, err)
		}
		_, err = service.UpdateOrganization(ctx, actor, org.UUID, OrganizationPatch{Change: Change{ExpectedVersion: org.Version, Reason: "旧资料"}, Name: new("不得覆盖")})
		if !errors.Is(err, ErrVersion) {
			t.Fatalf("旧版本未拒绝：%v", err)
		}
		org = updated
	})
	t.Run("不同操作者并发降级保留组织管理员", func(t *testing.T) {
		updated, err := service.GrantAdministrator(ctx, actor, org.UUID, AdministratorChange{Change: Change{ExpectedVersion: org.Version, Reason: "增加管理员"}, AccountUUID: a})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := database.ListPlatformAdminMembers(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org.UUID, Limit: 10})
		if err != nil || len(rows) != 2 {
			t.Fatalf("成员：%+v，%v", rows, err)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for i, account := range []string{a, b} {
			workers.Go(func() {
				<-start
				_, err := service.DemoteAdministrator(ctx, Actor{AccountUUID: account}, org.UUID, rows[i].UUID, AdministratorChange{Change: Change{ExpectedVersion: updated.Version, Reason: "并发降级"}, Role: "user"})
				results <- err
			})
		}
		close(start)
		workers.Wait()
		close(results)
		var success, conflicts int
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, ErrVersion) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflicts != 1 {
			t.Fatalf("降级结果 = %d 成功，%d 冲突", success, conflicts)
		}
		current, err := database.GetPlatformAdminOrganization(ctx, org.UUID)
		if err != nil || current.AdminCount != 1 {
			t.Fatalf("组织管理员数量：%+v，%v", current, err)
		}
		org = organizationDTO(current)
	})
	for _, entry := range []string{"Console", "Admin API"} {
		t.Run("后台与"+entry+"并发降级", func(t *testing.T) {
			target, _, err := service.CreateOrganization(ctx, actor, CreateOrganization{Name: entry + " 并发组织", Reason: "跨入口验收", AdminAccountUUIDs: []string{a, b}}, "race-"+entry)
			if err != nil {
				t.Fatal(err)
			}
			members, _, err := database.ListAdminUsersPage(ctx, db.ListAdminUsersParams{OrganizationUUID: target.UUID, Limit: 10})
			if err != nil || len(members) != 2 {
				t.Fatalf("组织初始成员错误：%+v，%v", members, err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var workers sync.WaitGroup
			workers.Go(func() {
				<-start
				_, err := service.DemoteAdministrator(ctx, actor, target.UUID, members[0].UUID, AdministratorChange{Change: Change{ExpectedVersion: target.Version, Reason: "后台并发降级"}, Role: "user"})
				results <- err
			})
			workers.Go(func() {
				<-start
				var err error
				if entry == "Console" {
					_, err = database.UpdateOrgUserRole(ctx, target.UUID, members[1].ExternalID, "user", members[1].UUID, func(role string) error {
						if role != "admin" {
							return ErrDenied
						}
						return nil
					})
				} else {
					_, err = database.UpdateAdminUserRole(ctx, target.UUID, members[1].ExternalID, "user")
				}
				results <- err
			})
			close(start)
			workers.Wait()
			close(results)
			var successes, protected int
			for err := range results {
				if err == nil {
					successes++
				} else if errors.Is(err, ErrVersion) || errors.Is(err, db.ErrLastOrganizationAdmin) {
					protected++
				} else {
					t.Fatalf("非预期并发结果：%v", err)
				}
			}
			current, err := database.GetPlatformAdminOrganization(ctx, target.UUID)
			if err != nil || successes != 1 || protected != 1 || current.AdminCount != 1 {
				t.Fatalf("跨入口保护失败：成功 %d，保护 %d，组织 %+v，%v", successes, protected, current, err)
			}
		})
	}

	t.Run("进行中的写入持有授权锁直到事务提交", func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		unlock := sync.OnceFunc(func() { close(release) })
		defer unlock()
		result := make(chan error, 1)
		go func() {
			result <- service.mutate(ctx, actor, func(tx *db.PlatformAdminTx) error {
				if _, err := tx.LockOrganization(ctx, org.UUID); err != nil {
					return err
				}
				close(entered)
				<-release
				return tx.UpdateOrganization(ctx, db.PlatformAdminOrganizationPatch{OrganizationUUID: org.UUID, Name: new("撤权前已获准的写入")})
			})
		}()
		select {
		case <-entered:
		case err := <-result:
			t.Fatalf("写入未能进入事务：%v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("等待写入授权锁超时")
		}
		revokeCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		err := service.SetAdministrator(revokeCtx, a, "测试运维", "与写入并发撤权", false)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("写入提交前撤权不应完成：%v", err)
		}
		unlock()
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		current, err := database.GetPlatformAdminOrganization(ctx, org.UUID)
		if err != nil || current.Name != "撤权前已获准的写入" {
			t.Fatalf("原事务未提交：%+v，%v", current, err)
		}
		org = organizationDTO(current)
		if err := service.SetAdministrator(ctx, a, "测试运维", "写入提交后撤权", false); err != nil {
			t.Fatal(err)
		}
		_, err = service.UpdateOrganization(ctx, actor, org.UUID, OrganizationPatch{Change: Change{ExpectedVersion: org.Version, Reason: "撤权后的写入"}, Name: new("不应写入")})
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("撤权提交后仍允许写入：%v", err)
		}
		if err := service.SetAdministrator(ctx, a, "测试运维", "恢复后续测试夹具", true); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("并发撤权保留最后超级管理员", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for _, account := range []string{a, b} {
			workers.Go(func() {
				<-start
				results <- service.SetAdministrator(ctx, account, "测试运维", "并发撤权", false)
			})
		}
		close(start)
		workers.Wait()
		close(results)
		var success, protected int
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, ErrLastAdmin) {
				protected++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || protected != 1 {
			t.Fatalf("撤权结果 = %d 成功，%d 保护", success, protected)
		}
		admins, err := service.Administrators(ctx)
		if err != nil || len(admins) != 1 {
			t.Fatalf("授权数量：%+v，%v", admins, err)
		}
		revoked := a
		if admins[0].UUID == a {
			revoked = b
		}
		_, err = service.UpdateOrganization(ctx, Actor{AccountUUID: revoked}, org.UUID, OrganizationPatch{Change: Change{ExpectedVersion: org.Version, Reason: "已撤权"}, Name: new("不得修改")})
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("已撤权仍可写入：%v", err)
		}
	})
}

func administrationService(t *testing.T, database *db.DB) *Service {
	t.Helper()
	var schema string
	if err := database.SQLDB().QueryRowContext(t.Context(), "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	migrator, err := rivermigrate.New(riverdatabasesql.New(database.SQLDB()), &rivermigrate.Config{Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Migrate(t.Context(), rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	client, err := river.NewClient[*sql.Tx](riverdatabasesql.New(database.SQLDB()), &river.Config{Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	service := New(database, nil)
	service.ConfigureDelivery(client)
	return service
}

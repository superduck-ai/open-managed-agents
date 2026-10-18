package db

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

func TestPlatformAdministrationPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("需要隔离 PostgreSQL 测试地址，跳过不代表验收通过")
	}
	ctx, sqlDB, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(sqlDB, yourbatis.DialectPostgres)}
	workspace := func() PlatformAuthWorkspaceInput {
		return PlatformAuthWorkspaceInput{UUID: uuid.NewV4().String(), ExternalID: "wrkspc_" + uuid.NewV4().String(), Name: "Default"}
	}
	t.Run("失败事务不保留组织和工作区", func(t *testing.T) {
		rollback := errors.New("主动回滚")
		var organization string
		err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			row, _, err := tx.CreateOrganization(ctx, PlatformAdminOrganizationInput{Name: "回滚组织", AccountUUID: uuid.NewV4().String(), Key: "rollback", Hash: "digest"}, workspace())
			organization = row.UUID
			if err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
		if _, err := store.GetPlatformAdminOrganization(ctx, organization); !errors.Is(err, ErrNotFound) {
			t.Fatalf("组织未回滚：%v", err)
		}
		rows, err := store.ListPlatformAdminWorkspaces(ctx, PlatformAdminPageQuery{OrganizationUUID: organization, Limit: 10})
		if err != nil || len(rows) != 0 {
			t.Fatalf("工作区未回滚：%+v，%v", rows, err)
		}
	})
	var organization string
	account := uuid.NewV4().String()
	member := uuid.NewV4().String()
	t.Run("创建目录和授权", func(t *testing.T) {
		err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			row, _, err := tx.CreateOrganization(ctx, PlatformAdminOrganizationInput{Name: "验收组织", AccountUUID: account, Key: "test-key", Hash: "digest"}, workspace())
			organization = row.UUID
			if err != nil {
				return err
			}
			return tx.InsertMember(ctx, PlatformAdminMemberInput{OrganizationUUID: organization, UUID: member, ExternalID: "user_member", Email: "member@example.com", Name: "组织管理员", Role: "admin"})
		})
		if err != nil {
			t.Fatal(err)
		}
		execMapperFixtureSQL(t, ctx, store.mapperDB, `INSERT INTO users (uuid, external_id, organization_uuid, email, role, registration_identity) VALUES ($1, 'user_account', $2, 'account@example.com', 'user', true)`, account, organization)
		err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			if err := tx.LockAdministration(ctx); err != nil {
				return err
			}
			if err := tx.SetGrant(ctx, PlatformAdminGrant{AccountUUID: account, Operator: "验收运维"}, true); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		identity, err := store.GetPlatformAdminAccount(ctx, account)
		if err != nil || !identity.Granted {
			t.Fatalf("授权未保存：%+v，%v", identity, err)
		}
		if _, err := store.GetPlatformAdminAccount(ctx, member); !errors.Is(err, ErrNotFound) {
			t.Fatalf("组织成员被误认为登录身份：%v", err)
		}
		rows, err := store.ListPlatformAdminWorkspaces(ctx, PlatformAdminPageQuery{OrganizationUUID: organization, Limit: 10})
		if err != nil || len(rows) != 1 || !rows[0].IsDefault {
			t.Fatalf("真实 Default 工作区：%+v，%v", rows, err)
		}
	})
	t.Run("资料版本和创建信息绑定", func(t *testing.T) {
		before, err := store.GetPlatformAdminOrganization(ctx, organization)
		if err != nil {
			t.Fatal(err)
		}
		err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			if _, err := tx.LockOrganization(ctx, organization); err != nil {
				return err
			}
			if err := tx.UpdateOrganization(ctx, PlatformAdminOrganizationPatch{OrganizationUUID: organization, Website: new("https://example.com")}); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		after, err := store.GetPlatformAdminOrganization(ctx, organization)
		if err != nil || after.Version <= before.Version || after.Website != "https://example.com" || after.Name != before.Name {
			t.Fatalf("资料更新：%+v，%v", after, err)
		}
		if err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			result, created, err := tx.CreateOrganization(ctx, PlatformAdminOrganizationInput{Name: "不能覆盖", AccountUUID: account, Key: "test-key", Hash: "different"}, workspace())
			if err != nil || created || result.Hash != "digest" || result.UUID != organization {
				t.Fatalf("幂等结果：%+v，%v，%v", result, created, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("组织唯一约束在独立事务并发下只创建一次", func(t *testing.T) {
		start := make(chan struct{})
		type outcome struct {
			row     PlatformAdminOrganizationCreation
			created bool
			err     error
		}
		results := make(chan outcome, 8)
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				<-start
				var result outcome
				result.err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
					var err error
					result.row, result.created, err = tx.CreateOrganization(ctx, PlatformAdminOrganizationInput{Name: "并发组织", AccountUUID: account, Key: "concurrent", Hash: "same"}, workspace())
					return err
				})
				results <- result
			})
		}
		close(start)
		wg.Wait()
		close(results)
		created, target := 0, ""
		for result := range results {
			if result.err != nil {
				t.Fatal(result.err)
			}
			if target == "" {
				target = result.row.UUID
			}
			if result.row.UUID != target || result.row.Hash != "same" {
				t.Fatalf("重复组织：%+v", result)
			}
			if result.created {
				created++
			}
		}
		rows, err := store.ListPlatformAdminWorkspaces(ctx, PlatformAdminPageQuery{OrganizationUUID: target, Limit: 10})
		if err != nil || created != 1 || len(rows) != 1 || !rows[0].IsDefault {
			t.Fatalf("重复创建：%d %+v %v", created, rows, err)
		}
	})
	t.Run("邀请与投递原子回滚和撤销", func(t *testing.T) {
		invitation := uuid.NewV4().String()
		input := PlatformAdminInviteInput{UUID: invitation, ExternalID: "invite_platform", OrganizationUUID: organization, AccountUUID: account, Email: "invited@example.com", Role: "admin"}
		rollback := errors.New("业务失败回滚")
		if err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			if _, err := tx.InsertInvite(ctx, input); err != nil {
				return err
			}
			if _, err := tx.EnqueueInviteDelivery(ctx, organization, invitation); err != nil {
				return err
			}
			return rollback
		}); !errors.Is(err, rollback) {
			t.Fatal(err)
		}
		rows, err := store.ListPlatformAdminInvites(ctx, PlatformAdminPageQuery{OrganizationUUID: organization, Limit: 10})
		if err != nil || len(rows) != 0 {
			t.Fatalf("邀请未回滚：%+v，%v", rows, err)
		}
		if err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			_, err := tx.InsertInvite(ctx, input)
			if err != nil {
				return err
			}
			_, err = tx.EnqueueInviteDelivery(ctx, organization, invitation)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		rows, err = store.ListPlatformAdminInvites(ctx, PlatformAdminPageQuery{OrganizationUUID: organization, Limit: 10})
		if err != nil || len(rows) != 1 || rows[0].DeliveryStatus != "pending" {
			t.Fatalf("投递未持久化：%+v，%v", rows, err)
		}
		snapshot := InvitationDelivery{InvitationUUID: invitation, OrganizationUUID: organization, Recipient: input.Email, InvitedAt: rows[0].InvitedAt, ExpiresAt: rows[0].ExpiresAt}
		found, err := store.StartInvitationDelivery(ctx, snapshot)
		if err != nil || !found {
			t.Fatalf("开始投递：%v %v", found, err)
		}
		if err := store.FinishInvitationDelivery(ctx, snapshot, "failed"); err != nil {
			t.Fatal(err)
		}
		if found, err := store.StartInvitationDelivery(ctx, snapshot); err != nil || !found {
			t.Fatalf("允许 River 重试：%v %v", found, err)
		}

		if err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			if err := tx.ResendInvite(ctx, organization, invitation); err != nil {
				return err
			}
			_, err := tx.EnqueueInviteDelivery(ctx, organization, invitation)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.FinishInvitationDelivery(ctx, snapshot, "sent"); err != nil {
			t.Fatal(err)
		}
		if found, err := store.StartInvitationDelivery(ctx, snapshot); err != nil || found {
			t.Fatalf("重发后旧任务仍有效：%v %v", found, err)
		}
		rows, err = store.ListPlatformAdminInvites(ctx, PlatformAdminPageQuery{OrganizationUUID: organization, Limit: 10})
		if err != nil || rows[0].DeliveryStatus != "pending" {
			t.Fatalf("旧结果覆盖重发状态：%+v %v", rows, err)
		}
		snapshot.InvitedAt, snapshot.ExpiresAt = rows[0].InvitedAt, rows[0].ExpiresAt
		for _, stale := range []InvitationDelivery{
			{InvitationUUID: snapshot.InvitationUUID, OrganizationUUID: uuid.NewV4().String(), Recipient: snapshot.Recipient, InvitedAt: snapshot.InvitedAt, ExpiresAt: snapshot.ExpiresAt},
			{InvitationUUID: snapshot.InvitationUUID, OrganizationUUID: snapshot.OrganizationUUID, Recipient: "wrong@example.com", InvitedAt: snapshot.InvitedAt, ExpiresAt: snapshot.ExpiresAt},
			{InvitationUUID: snapshot.InvitationUUID, OrganizationUUID: snapshot.OrganizationUUID, Recipient: snapshot.Recipient, InvitedAt: snapshot.InvitedAt.Add(time.Second), ExpiresAt: snapshot.ExpiresAt},
		} {
			if found, err := store.StartInvitationDelivery(ctx, stale); err != nil || found {
				t.Fatalf("错误快照被执行：%+v %v", stale, err)
			}
		}
		if found, err := store.StartInvitationDelivery(ctx, snapshot); err != nil || !found {
			t.Fatalf("新任务不能投递：%v %v", found, err)
		}
		wrong := snapshot
		wrong.OrganizationUUID = uuid.NewV4().String()
		if err := store.FinishInvitationDelivery(ctx, wrong, "sent"); err != nil {
			t.Fatal(err)
		}
		current, err := store.ListPlatformAdminInvites(ctx, PlatformAdminPageQuery{OrganizationUUID: organization, Limit: 10})
		if err != nil || current[0].DeliveryStatus != "sending" {
			t.Fatalf("跨组织结果被写入：%+v %v", current, err)
		}
		if _, err := store.ResendConsoleInvite(ctx, organization, input.ExternalID); err != nil {
			t.Fatal(err)
		}
		if err := store.FinishInvitationDelivery(ctx, snapshot, "sent"); err != nil {
			t.Fatal(err)
		}
		rows, err = store.ListPlatformAdminInvites(ctx, PlatformAdminPageQuery{OrganizationUUID: organization, Limit: 10})
		if err != nil || rows[0].DeliveryStatus != "not_queued" {
			t.Fatalf("Console 重发保留了旧平台结果：%+v %v", rows, err)
		}
		if err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			_, err := tx.EnqueueInviteDelivery(ctx, organization, invitation)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		snapshot.InvitedAt, snapshot.ExpiresAt = rows[0].InvitedAt, rows[0].ExpiresAt
		if found, err := store.StartInvitationDelivery(ctx, snapshot); err != nil || !found {
			t.Fatalf("撤销前任务状态：%v %v", found, err)
		}
		if err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			pending, err := tx.HasPendingInvite(ctx, organization, input.Email)
			if err != nil {
				return err
			}
			if !pending {
				return errors.New("未识别待接受邀请")
			}
			if _, err := tx.Invite(ctx, organization, invitation); err != nil {
				return err
			}
			return tx.RevokeInvite(ctx, organization, invitation)
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.FinishInvitationDelivery(ctx, snapshot, "sent"); err != nil {
			t.Fatal(err)
		}
		rows, err = store.ListPlatformAdminInvites(ctx, PlatformAdminPageQuery{OrganizationUUID: organization, Limit: 10})
		if err != nil || len(rows) != 1 || rows[0].Status != "deleted" || rows[0].DeliveryStatus != "cancelled" {
			t.Fatalf("撤销未取消投递：%+v，%v", rows, err)
		}
		if found, err := store.StartInvitationDelivery(ctx, snapshot); err != nil || found {
			t.Fatalf("撤销后仍允许投递：%v %v", found, err)
		}
		if _, err := store.RespondToInvitation(ctx, input.ExternalID, input.Email, true); !errors.Is(err, ErrInvitationRevoked) {
			t.Fatalf("撤销后仍能接受邀请：%v", err)
		}
	})
	t.Run("失效邀请快照不得执行或回写", func(t *testing.T) {
		for _, state := range []string{"accepted", "declined", "expired"} {
			t.Run(state, func(t *testing.T) {
				snapshot := InvitationDelivery{InvitationUUID: uuid.NewV4().String(), OrganizationUUID: organization,
					Recipient: state + "@example.com", InvitedAt: time.Now().UTC().Truncate(time.Microsecond), ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
				status := state
				if state == "expired" {
					status = "pending"
					snapshot.ExpiresAt = snapshot.InvitedAt.Add(-time.Minute)
					snapshot.InvitedAt = snapshot.InvitedAt.Add(-24 * time.Hour)
				}
				if _, err := sqlDB.ExecContext(ctx, `INSERT INTO organization_invites (uuid, external_id, organization_uuid, email, role, status, invited_at, expires_at, email_delivery_status)
 VALUES ($1,$2,$3,$4,'user',$5,$6,$7,'sending')`, snapshot.InvitationUUID, "invite_"+snapshot.InvitationUUID, organization, snapshot.Recipient, status, snapshot.InvitedAt, snapshot.ExpiresAt); err != nil {
					t.Fatal(err)
				}
				if found, err := store.StartInvitationDelivery(ctx, snapshot); err != nil || found {
					t.Fatalf("失效邀请仍被发送：%v %v", found, err)
				}
				if err := store.FinishInvitationDelivery(ctx, snapshot, "sent"); err != nil {
					t.Fatal(err)
				}
				var result string
				if err := sqlDB.QueryRowContext(ctx, "SELECT email_delivery_status FROM organization_invites WHERE uuid=$1", snapshot.InvitationUUID).Scan(&result); err != nil || result != "sending" {
					t.Fatalf("失效邀请仍被回写：%s %v", result, err)
				}
			})
		}
	})

}

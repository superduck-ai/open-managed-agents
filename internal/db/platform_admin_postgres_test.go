package db

import (
	"errors"
	"os"
	"testing"
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
			var err error
			organization, err = tx.CreateOrganization(ctx, "回滚组织", workspace())
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
			var err error
			organization, err = tx.CreateOrganization(ctx, "验收组织", workspace())
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
	t.Run("资料版本和幂等 JSON 绑定", func(t *testing.T) {
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
			return tx.SaveCreateRequest(ctx, PlatformAdminCreateRequest{AccountUUID: account, Key: "test-key", Hash: "digest", Response: []byte(`{"created":true}`)})
		})
		if err != nil {
			t.Fatal(err)
		}
		after, err := store.GetPlatformAdminOrganization(ctx, organization)
		if err != nil || after.Version <= before.Version || after.Website != "https://example.com" || after.Name != before.Name {
			t.Fatalf("资料更新：%+v，%v", after, err)
		}
		if err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			result, found, err := tx.CreateRequest(ctx, account, "test-key")
			if err != nil || !found || result.Hash != "digest" || len(result.Response) == 0 {
				t.Fatalf("幂等结果：%+v，%v，%v", result, found, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
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
		var count int
		if err := sqlDB.QueryRowContext(ctx, "SELECT count(*) FROM platform_admin_invite_deliveries WHERE invitation_uuid = $1", invitation).Scan(&count); err != nil || count != 0 {
			t.Fatalf("投递未回滚：%d，%v", count, err)
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
		var deliveryUUID string
		if err := sqlDB.QueryRowContext(ctx, "SELECT uuid FROM platform_admin_invite_deliveries WHERE invitation_uuid = $1", invitation).Scan(&deliveryUUID); err != nil {
			t.Fatal(err)
		}
		delivery, found, err := store.StartPlatformAdminDelivery(ctx, deliveryUUID)
		if err != nil || !found {
			t.Fatalf("开始投递：%+v %v %v", delivery, found, err)
		}
		if err := store.FinishPlatformAdminDelivery(ctx, delivery.UUID, "failed"); err != nil {
			t.Fatal(err)
		}
		if _, found, err := store.StartPlatformAdminDelivery(ctx, deliveryUUID); err != nil || !found {
			t.Fatalf("允许 River 重试：%v %v", found, err)
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
		rows, err = store.ListPlatformAdminInvites(ctx, PlatformAdminPageQuery{OrganizationUUID: organization, Limit: 10})
		if err != nil || len(rows) != 1 || rows[0].Status != "deleted" || rows[0].DeliveryStatus != "cancelled" {
			t.Fatalf("撤销未取消投递：%+v，%v", rows, err)
		}
		if _, found, err := store.StartPlatformAdminDelivery(ctx, deliveryUUID); err != nil || found {
			t.Fatalf("撤销后仍允许投递：%v %v", found, err)
		}
		if _, err := store.RespondToInvitation(ctx, input.ExternalID, input.Email, true); !errors.Is(err, ErrInvitationRevoked) {
			t.Fatalf("撤销后仍能接受邀请：%v", err)
		}
	})
}

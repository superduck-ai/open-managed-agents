//go:build integration

package platformadmin

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestAdministrationMemberChangesPostgres(t *testing.T) {
	database := administrationDatabase(t)
	service := administrationService(t, database)
	ctx := t.Context()
	operator, first, candidate := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	err := database.WithPlatformAuthTx(ctx, func(tx db.PlatformAuthTxStore) error {
		org, err := tx.InsertOrganization(ctx, db.PlatformAuthOrganizationInput{Name: "注册身份"})
		if err != nil {
			return err
		}
		for _, account := range []string{operator, first, candidate} {
			if _, err := tx.InsertUser(ctx, db.PlatformAuthUserInput{UUID: account, ExternalID: "user_" + account, OrganizationUUID: org.UUID, Email: account + "@example.test", Name: "成员" + account, Role: "admin"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetAdministrator(ctx, operator, "测试运维", "", true); err != nil {
		t.Fatal(err)
	}
	actor := Actor{AccountUUID: operator}
	org, _, err := service.CreateOrganization(ctx, actor, CreateOrganization{Name: "成员验收组织", AdminAccountUUIDs: []string{first}}, "members")
	if err != nil {
		t.Fatal(err)
	}
	change := func(role string) MemberChange {
		return MemberChange{Change: Change{ExpectedVersion: org.Version}, AccountUUID: candidate, Role: role}
	}
	rows, err := database.ListPlatformAdminMembers(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org.UUID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	original := rows[0].UUID
	t.Run("拒绝非法角色及账号且不改变版本", func(t *testing.T) {
		if _, err := service.AddMember(ctx, actor, org.UUID, change("super_admin")); err == nil {
			t.Fatal("接受非法角色")
		}
		input := change("user")
		input.AccountUUID = uuid.NewV4().String()
		if _, err := service.AddMember(ctx, actor, org.UUID, input); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("不存在的账号：%v", err)
		}
		if _, err := service.ChangeMember(ctx, actor, org.UUID, original, change("user"), true); !errors.Is(err, ErrLastOrganizationAdmin) {
			t.Fatalf("最后管理员：%v", err)
		}
		if _, err := service.ChangeMember(ctx, actor, org.UUID, candidate, change("user"), false); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("跨组织标识：%v", err)
		}
	})
	t.Run("直接添加立即生效且不创建邀请", func(t *testing.T) {
		updated, err := service.AddMember(ctx, actor, org.UUID, change("user"))
		if err != nil {
			t.Fatal(err)
		}
		org = updated
		if org.MemberCount != 2 || org.AdminCount != 1 {
			t.Fatalf("人数错误：%+v", org)
		}
		invitations, err := database.ListPlatformAdminInvites(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org.UUID, Limit: 10})
		if err != nil || len(invitations) != 0 {
			t.Fatalf("不应创建邀请：%+v %v", invitations, err)
		}
	})
	t.Run("重复添加不覆盖角色且旧版本拒绝", func(t *testing.T) {
		if _, err := service.AddMember(ctx, actor, org.UUID, change("admin")); !errors.Is(err, ErrMemberExists) {
			t.Fatalf("重复成员：%v", err)
		}
		input := change("user")
		input.ExpectedVersion = 1
		if _, err := service.ChangeMember(ctx, actor, org.UUID, original, input, false); !errors.Is(err, ErrVersion) {
			t.Fatalf("旧版本：%v", err)
		}
	})
	t.Run("服务端按组织搜索角色与分页", func(t *testing.T) {
		rows, err := database.ListPlatformAdminMembers(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org.UUID, Query: candidate, Role: "user", Limit: 10})
		if err != nil || len(rows) != 1 {
			t.Fatalf("筛选错误：%+v %v", rows, err)
		}
		if rows[0].UUID == candidate {
			t.Fatal("成员UUID错误复用登录账号")
		}
		page, err := database.ListPlatformAdminMembers(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org.UUID, After: rows[0].UUID, Query: candidate, Role: "user", Limit: 10})
		if err != nil || len(page) != 0 {
			t.Fatalf("游标错误：%+v %v", page, err)
		}
	})
	t.Run("先提升再降级移除与重新加入", func(t *testing.T) {
		rows, err := database.ListPlatformAdminMembers(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org.UUID, Role: "user", Limit: 10})
		if err != nil || len(rows) != 1 {
			t.Fatalf("成员：%+v %v", rows, err)
		}
		updated, err := service.ChangeMember(ctx, actor, org.UUID, rows[0].UUID, change("admin"), false)
		if err != nil {
			t.Fatal(err)
		}
		org = updated
		updated, err = service.ChangeMember(ctx, actor, org.UUID, original, change("user"), false)
		if err != nil {
			t.Fatal(err)
		}
		org = updated
		workspace, err := database.CreateAdminWorkspace(ctx, db.AdminWorkspace{UUID: uuid.NewV4().String(), ExternalID: "wrkspc_member_changes", OrganizationUUID: org.UUID, Name: "显式空间", DataResidency: []byte(`{}`), Tags: []byte(`[]`), CompartmentID: uuid.NewV4().String(), CreatedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		member, err := database.FindOrgMemberByReference(ctx, org.UUID, original)
		if err != nil {
			t.Fatal(err)
		}
		_, err = database.CreateAdminWorkspaceMember(ctx, db.AdminWorkspaceMember{ExternalID: "wsm_member_changes", OrganizationUUID: org.UUID, WorkspaceUUID: workspace.UUID, WorkspaceExternalID: workspace.ExternalID, UserUUID: original, UserExternalID: member.ExternalID, WorkspaceRole: "workspace_user", CreatedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		updated, err = service.ChangeMember(ctx, actor, org.UUID, original, change("user"), true)
		if err != nil {
			t.Fatal(err)
		}
		org = updated
		if _, err := database.GetAdminWorkspaceMember(ctx, org.UUID, workspace.ExternalID, member.ExternalID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("显式工作区关系未清理：%v", err)
		}
		if _, err := database.GetAdminWorkspace(ctx, org.UUID, workspace.ExternalID); err != nil {
			t.Fatalf("错误删除空间资源：%v", err)
		}
		if _, err := database.GetPlatformAdminAccount(ctx, first); err != nil {
			t.Fatalf("错误删除稳定登录身份：%v", err)
		}
		if _, err := service.ChangeMember(ctx, actor, org.UUID, original, change("user"), true); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("重复移除：%v", err)
		}
		input := change("user")
		input.AccountUUID = first
		updated, err = service.AddMember(ctx, actor, org.UUID, input)
		if err != nil {
			t.Fatal(err)
		}
		org = updated
		rows, err = database.ListPlatformAdminMembers(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org.UUID, Role: "user", Limit: 10})
		if err != nil || len(rows) != 1 || rows[0].UUID == original {
			t.Fatalf("恢复了旧成员：%+v %v", rows, err)
		}
	})
}

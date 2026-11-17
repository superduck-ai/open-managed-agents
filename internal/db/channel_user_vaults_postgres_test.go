//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

func TestChannelUserVaultsPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	fixture := devIdentityFixture()
	if err := store.CreateDevIdentity(ctx, fixture); err != nil {
		t.Fatal(err)
	}
	identity := ChannelUserVaultIdentity{OrganizationUUID: fixture.OrganizationUUID, WorkspaceUUID: fixture.Workspaces[0].UUID, ChannelType: "feishu", AppID: "test-app", ActorID: "user-a"}
	var group sync.WaitGroup
	var lock sync.Mutex
	results := map[string]bool{}
	for range 8 {
		group.Go(func() {
			id, err := ids.New("vlt_")
			if err != nil {
				t.Error(err)
				return
			}
			got, err := store.EnsureChannelUserVault(ctx, identity, Vault{UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: identity.OrganizationUUID, WorkspaceUUID: identity.WorkspaceUUID, DisplayName: "用户关联测试", Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
			if err != nil {
				t.Error(err)
				return
			}
			lock.Lock()
			results[got] = true
			lock.Unlock()
		})
	}
	group.Wait()
	if len(results) != 1 {
		t.Fatalf("同一用户创建了多个 Vault：%v", results)
	}
	a, err := store.GetChannelUserVaultID(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	identity.ActorID = "user-b"
	bID, err := ids.New("vlt_")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.EnsureChannelUserVault(ctx, identity, Vault{UUID: uuid.NewV4().String(), ExternalID: bID, OrganizationUUID: identity.OrganizationUUID, WorkspaceUUID: identity.WorkspaceUUID, DisplayName: "用户 B", Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err != nil || a == b {
		t.Fatalf("不同用户复用了 Vault：%v", err)
	}
	page, more, err := store.ListVaultsPage(ctx, ListVaultsPageParams{WorkspaceUUID: identity.WorkspaceUUID, Limit: 10})
	if err != nil || more || len(page) != 2 {
		t.Fatalf("并发创建遗留孤立 Vault：%d %v", len(page), err)
	}
	identity.OrganizationUUID = uuid.NewV4().String()
	if _, err := store.GetChannelUserVaultID(ctx, identity); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨组织读取关联：%v", err)
	}
	identity.OrganizationUUID = fixture.OrganizationUUID
	identity.ActorID = "user-c"
	owned, err := store.GetVault(ctx, identity.WorkspaceUUID, a)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewChannelUserVaultMapper(store.mapperDB).Insert(ctx, identity, owned.UUID); err == nil {
		t.Fatal("同一 Vault 被关联给不同用户")
	}
	identity.ActorID = "user-b"
	identity.AppID = "other-app"
	if _, err := store.GetChannelUserVaultID(ctx, identity); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨应用读取关联：%v", err)
	}
	identity.AppID = "test-app"
	identity.WorkspaceUUID = fixture.Workspaces[1].UUID
	if _, err := store.GetChannelUserVaultID(ctx, identity); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨工作区读取关联：%v", err)
	}
	code := seedClaimCodeSession(t, ctx, database, 98, "claim", "idle")
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET organization_uuid=$1,workspace_uuid=$2 WHERE external_id=$3`, fixture.OrganizationUUID, fixture.Workspaces[0].UUID, code)
	execClaimTest(t, ctx, database, `UPDATE sessions SET organization_uuid=$1,workspace_uuid=$2,vault_ids=jsonb_build_array($3::text,$4::text),metadata=jsonb_build_object('credential_binding_mode','first_vault','oauth_vault_id',$4::text) WHERE external_id='sesn_claim_98'`, fixture.OrganizationUUID, fixture.Workspaces[0].UUID, a, b)
	scope, err := store.GetCodeSessionCredentialScope(ctx, code, fixture.OrganizationUUID, fixture.Workspaces[0].UUID)
	if err != nil || !scope.PinBindings || scope.OAuthVaultID != b || len(scope.VaultIDs) != 2 || scope.VaultIDs[0] != a {
		t.Fatalf("原生凭据范围扫描错误：%+v %v", scope, err)
	}
	execClaimTest(t, ctx, database, `UPDATE vaults SET archived_at=now() WHERE external_id=$1`, a)
	if _, err := store.GetCodeSessionCredentialScope(ctx, code, fixture.OrganizationUUID, fixture.Workspaces[0].UUID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("共享挂载归档后不能切换其他连接：%v", err)
	}
}

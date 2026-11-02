//go:build integration

package db

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

type channelMigrationTarget struct{ secrets.KeyProvider }

func (p channelMigrationTarget) Name() string { return "hashicorp_vault" }

func TestChannelWorkspaceCredentialsPostgreSQL(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	identity := devIdentityFixture()
	if err := store.CreateDevIdentity(ctx, identity); err != nil {
		t.Fatal(err)
	}
	otherIdentity := devIdentityFixture()
	otherIdentity.User.ExternalID, otherIdentity.User.Email = "user_channel_other", "channel-other@oma.ai"
	for i := range otherIdentity.Workspaces {
		otherIdentity.Workspaces[i].ExternalID += "_other"
	}
	if err := store.CreateDevIdentity(ctx, otherIdentity); err != nil {
		t.Fatal(err)
	}
	provider, err := localkeys.New(localkeys.KeyMaterial{Version: 1, KEK: make([]byte, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cipher := secrets.NewService(provider)
	input := platform.CreateConsoleAPIKeyInput{OrgUUID: identity.OrganizationUUID, WorkspaceUUID: identity.Workspaces[0].UUID}
	t.Run("跨组织不能签发工作区身份", func(t *testing.T) {
		wrong := input
		wrong.OrgUUID = otherIdentity.OrganizationUUID
		if _, err := store.EnsureChannelWorkspaceCredential(ctx, wrong, cipher); !errors.Is(err, ErrNotFound) {
			t.Fatalf("另一合法组织应返回工作区不存在: %v", err)
		}
	})
	t.Run("已归档工作区不可签发凭据", func(t *testing.T) {
		archived := input
		archived.WorkspaceUUID = identity.Workspaces[1].UUID
		if _, err := database.ExecContext(ctx, `UPDATE workspaces SET archived_at=now() WHERE organization_uuid=$1 AND uuid=$2`, input.OrgUUID, archived.WorkspaceUUID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.EnsureChannelWorkspaceCredential(ctx, archived, cipher); !errors.Is(err, ErrNotFound) {
			t.Fatalf("已归档工作区应返回不存在: %v", err)
		}
		if _, err := database.ExecContext(ctx, `UPDATE workspaces SET archived_at=NULL WHERE organization_uuid=$1 AND uuid=$2`, input.OrgUUID, archived.WorkspaceUUID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("身份与 Key 写入失败时整体回滚", func(t *testing.T) {
		if _, err := database.ExecContext(ctx, `ALTER TABLE channel_workspace_credentials ADD CONSTRAINT test_reject_credentials CHECK (false) NOT VALID`); err != nil {
			t.Fatal(err)
		}
		_, err := store.EnsureChannelWorkspaceCredential(ctx, input, cipher)
		if err == nil {
			t.Fatal("约束失败仍提交身份")
		}
		if _, err := database.ExecContext(ctx, `ALTER TABLE channel_workspace_credentials DROP CONSTRAINT test_reject_credentials`); err != nil {
			t.Fatal(err)
		}
		keys, err := store.ListConsoleAPIKeys(ctx, input.OrgUUID, &input.WorkspaceUUID)
		if err != nil || len(keys) != 0 {
			t.Fatalf("失败后存在孤立 Key，数量=%d，错误=%v", len(keys), err)
		}
	})
	var first string
	t.Run("首次创建且重试与多请求复用同一身份", func(t *testing.T) {
		var err error
		first, err = store.EnsureChannelWorkspaceCredential(ctx, input, cipher)
		if err != nil || !strings.HasPrefix(first, "sk-ant-api03-") {
			t.Fatalf("创建失败: %v", err)
		}
		var group sync.WaitGroup
		for range 4 {
			group.Go(func() {
				key, err := store.EnsureChannelWorkspaceCredential(ctx, input, cipher)
				if err != nil || key != first {
					t.Errorf("重复请求未复用原凭据: %v", err)
				}
			})
		}
		group.Wait()
		var encoded string
		if err := database.QueryRowContext(ctx, `SELECT envelope FROM channel_workspace_credentials WHERE organization_uuid=$1 AND workspace_uuid=$2`, input.OrgUUID, input.WorkspaceUUID).Scan(&encoded); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(encoded, first) {
			t.Fatal("数据库保存了明文 Key")
		}
		keys, err := store.ListConsoleAPIKeys(ctx, input.OrgUUID, &input.WorkspaceUUID)
		if err != nil || len(keys) != 1 {
			t.Fatalf("专用 Key 数量=%d，错误=%v", len(keys), err)
		}
	})
	t.Run("迁移整份信封后只保留新 Provider 仍可读取凭据", func(t *testing.T) {
		rows, err := store.ListSecretMigrationPage(ctx, "channel_workspace_credentials", "")
		if err != nil || len(rows) != 1 {
			t.Fatalf("迁移扫描失败: %v", err)
		}
		var original secrets.Envelope
		if err := json.Unmarshal(rows[0].Document, &original); err != nil {
			t.Fatal(err)
		}
		localTarget, err := localkeys.New(localkeys.KeyMaterial{Version: 2, KEK: bytes.Repeat([]byte{2}, 32)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		target := channelMigrationTarget{KeyProvider: localTarget}
		migrated, err := secrets.NewService(target, provider).MigrateEnvelope(ctx, original)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(migrated.Ciphertext, original.Ciphertext) || !bytes.Equal(migrated.Nonce, original.Nonce) {
			t.Fatal("迁移改变业务密文")
		}
		next, err := json.Marshal(migrated)
		if err != nil {
			t.Fatal(err)
		}
		wrong := rows[0]
		wrong.OrganizationUUID = otherIdentity.OrganizationUUID
		if err := store.SaveMigratedSecret(ctx, "channel_workspace_credentials", wrong, next); !errors.Is(err, ErrVersionConflict) {
			t.Fatalf("跨组织迁移未被拒绝: %v", err)
		}
		if err := store.SaveMigratedSecret(ctx, "channel_workspace_credentials", rows[0], next); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveMigratedSecret(ctx, "channel_workspace_credentials", rows[0], next); !errors.Is(err, ErrVersionConflict) {
			t.Fatalf("过期 CAS 未被拒绝: %v", err)
		}
		cipher = secrets.NewService(target)
		key, err := store.EnsureChannelWorkspaceCredential(ctx, input, cipher)
		if err != nil || key != first {
			t.Fatalf("移除旧 Provider 后无法读取原凭据: %v", err)
		}
	})
	t.Run("不同工作区身份独立且密文不能换工作区解密", func(t *testing.T) {
		other := input
		other.WorkspaceUUID = identity.Workspaces[1].UUID
		key, err := store.EnsureChannelWorkspaceCredential(ctx, other, cipher)
		if err != nil || key == first {
			t.Fatalf("工作区凭据未隔离: %v", err)
		}
		if _, err := database.ExecContext(ctx, `UPDATE channel_workspace_credentials SET envelope=(SELECT envelope FROM channel_workspace_credentials WHERE organization_uuid=$1 AND workspace_uuid=$2) WHERE organization_uuid=$1 AND workspace_uuid=$3`, input.OrgUUID, input.WorkspaceUUID, other.WorkspaceUUID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.EnsureChannelWorkspaceCredential(ctx, other, cipher); !errors.Is(err, ErrChannelCredentialUnreadable) {
			t.Fatalf("跨工作区密文应返回明确解密错误: %v", err)
		}
	})
	t.Run("停用 Key 后不自动新建", func(t *testing.T) {
		keys, err := store.ListConsoleAPIKeys(ctx, input.OrgUUID, &input.WorkspaceUUID)
		if err != nil || len(keys) != 1 {
			t.Fatal("专用 Key 未创建")
		}
		if _, err := store.UpdateConsoleAPIKeyStatus(ctx, platform.UpdateConsoleAPIKeyStatusInput{OrgUUID: input.OrgUUID, WorkspaceUUID: input.WorkspaceUUID, APIKeyID: keys[0].ID, Status: "inactive"}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.EnsureChannelWorkspaceCredential(ctx, input, cipher); !errors.Is(err, ErrChannelCredentialInactive) {
			t.Fatalf("停用后未拒绝使用: %v", err)
		}
	})
}

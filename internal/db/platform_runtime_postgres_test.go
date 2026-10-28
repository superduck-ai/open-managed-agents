//go:build integration

package db

import (
	"errors"
	"testing"
)

func TestPlatformRuntimeFilestoreScopePostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	const home = "63000000-0000-4000-8000-000000000001"
	const target = "63000000-0000-4000-8000-000000000002"
	const workspace = "63000000-0000-4000-8000-000000000003"
	const account = "63000000-0000-4000-8000-000000000004"
	const stranger = "63000000-0000-4000-8000-000000000005"
	const member = "63000000-0000-4000-8000-000000000006"
	const session = "63000000-0000-4000-8000-000000000007"
	const filesystem = "63000000-0000-4000-8000-000000000008"
	execFixture := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	execFixture(`INSERT INTO organizations(uuid,name) VALUES ($1,'来源'),($2,'目标')`, home, target)
	execFixture(`INSERT INTO workspaces(uuid,external_id,organization_uuid,name,is_default) VALUES ($1,'wrkspc_runtime',$2,'Default',true)`, workspace, target)
	for _, user := range []struct{ id, org, name string }{{account, home, "account"}, {stranger, home, "stranger"}, {member, target, "member"}} {
		execFixture(`INSERT INTO users(uuid,external_id,organization_uuid,email,name,role) VALUES ($1,$2,$3,$4,$2,'user')`, user.id, "user_"+user.name, user.org, user.name+"@example.com")
	}
	execFixture(`INSERT INTO sessions(uuid,external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,status,metadata) VALUES ($1,'sesn_runtime',$2,$3,'63000000-0000-4000-8000-000000000009','env_runtime','63000000-0000-4000-8000-000000000010','agent_runtime',1,'{}','idle',$4)`, session, target, workspace, `{"_oma_runtime_user_uuid":"`+account+`"}`)
	execFixture(`INSERT INTO filestore_filesystems(uuid,external_id,organization_uuid,workspace_uuid,session_uuid) VALUES ($1,'claude_chat_runtime',$2,$3,$4)`, filesystem, target, workspace, session)
	execFixture(`INSERT INTO code_sessions(external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,environment_uuid,environment_external_id,status) VALUES ('cse_runtime',$1,$2,$3,'sesn_runtime','63000000-0000-4000-8000-000000000009','env_runtime','active')`, target, workspace, session)
	validate := func(user string) error {
		_, err := store.ResolveFilestoreTokenScope(ctx, target, user, workspace, "wrkspc_runtime", "wrkspc_runtime", "claude_chat_runtime")
		return err
	}
	t.Run("缺少隐藏运行身份不能访问跨组织账号范围", func(t *testing.T) {
		execFixture(`UPDATE sessions SET metadata='{}' WHERE uuid=$1`, session)
		defer execFixture(`UPDATE sessions SET metadata=$1 WHERE uuid=$2`, `{"_oma_runtime_user_uuid":"`+account+`"}`, session)
		if err := validate(account); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("不能冒用其他跨组织账号", func(t *testing.T) {
		if err := validate(stranger); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("不能替换目标工作区", func(t *testing.T) {
		if _, err := store.GetFilestoreTokenScopeForSessionIssue(ctx, home, "sesn_runtime"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	})
	for _, change := range []struct{ name, apply, restore string }{
		{"运行账号失效仍拒绝", `UPDATE users SET deleted_at=now() WHERE uuid='` + account + `'`, `UPDATE users SET deleted_at=NULL WHERE uuid='` + account + `'`},
		{"工作区归档仍拒绝", `UPDATE workspaces SET archived_at=now() WHERE uuid='` + workspace + `'`, `UPDATE workspaces SET archived_at=NULL WHERE uuid='` + workspace + `'`},
		{"会话终止仍拒绝", `UPDATE sessions SET status='terminated' WHERE uuid='` + session + `'`, `UPDATE sessions SET status='idle' WHERE uuid='` + session + `'`},
	} {
		t.Run(change.name, func(t *testing.T) {
			execFixture(change.apply)
			defer execFixture(change.restore)
			if _, err := store.GetFilestoreTokenScopeForSessionIssue(ctx, workspace, "sesn_runtime"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("签发 err=%v", err)
			}
			if err := validate(account); !errors.Is(err, ErrNotFound) {
				t.Fatalf("回查 err=%v", err)
			}
		})
	}
	t.Run("平台稳定账号通过持久化运行身份闭合目标资源范围", func(t *testing.T) {
		scope, err := store.GetFilestoreTokenScopeForSessionIssue(ctx, workspace, "sesn_runtime")
		if err != nil || scope.AccountUUID != account || scope.OrganizationUUID != target || scope.WorkspaceUUID != workspace {
			t.Fatalf("scope=%+v err=%v", scope, err)
		}
		if err := validate(account); err != nil {
			t.Fatal(err)
		}
		credential, err := NewCodeSessionMapper(store.mapperDB).FindCredentialForIssue(ctx, target, workspace, "cse_runtime")
		if err != nil || credential.AccountEmail != "account@example.com" {
			t.Fatalf("credential=%+v err=%v", credential, err)
		}
	})
	t.Run("原目标组织成员的签名凭据权限不变", func(t *testing.T) {
		if err := validate(member); err != nil {
			t.Fatal(err)
		}
	})
}

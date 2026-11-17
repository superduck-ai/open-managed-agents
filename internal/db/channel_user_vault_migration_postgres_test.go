//go:build integration

package db

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
)

func TestChannelUserVaultMigrationPreservesIdentityAndGroup(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("未配置 PostgreSQL 集成测试地址")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.UpTo(ctx, 155); err != nil {
		t.Fatal(err)
	}
	legacyID := fmt.Sprintf("vlt_channel_%x", sha256.Sum256([]byte(claimTestOrganization+"\x00"+claimTestWorkspace+"\x00feishu\x00test-app\x00user-a")))
	execClaimTest(t, ctx, database, `INSERT INTO vaults (external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid, display_name, metadata)
VALUES ($1,$2,$3,gen_random_uuid(),'旧个人连接','{"source":"channel-oauth","channel_type":"feishu","app_id":"test-app","actor_id":"user-a"}'),
('vlt_missing',$2,$3,gen_random_uuid(),'缺失身份','{"source":"channel-oauth"}'),
('vlt_forged',$2,$3,gen_random_uuid(),'复制身份','{"source":"channel-oauth","channel_type":"feishu","app_id":"test-app","actor_id":"user-a"}'),
('vlt_channel_0000000000000000000000000000000000000000000000000000000000000000',$2,$3,gen_random_uuid(),'伪造旧标识','{"source":"channel-oauth","channel_type":"feishu","app_id":"test-app","actor_id":"user-a"}')`, legacyID, claimTestOrganization, claimTestWorkspace)
	for _, index := range []int{301, 302, 303} {
		seedClaimCodeSession(t, ctx, database, index, "claim", "idle")
	}
	execClaimTest(t, ctx, database, `UPDATE sessions SET vault_ids=jsonb_build_array($1::text), metadata='{"source":"feishu","app_id":"test-app","actor_id":"user-a","chat_type":"p2p"}' WHERE external_id='sesn_claim_301'`, legacyID)
	execClaimTest(t, ctx, database, `UPDATE sessions SET vault_ids='["vlt_shared"]', metadata='{"source":"feishu","app_id":"test-app","chat_type":"group"}' WHERE external_id IN ('sesn_claim_302','sesn_claim_303')`)
	execClaimTest(t, ctx, database, `INSERT INTO channels (external_id,organization_uuid,workspace_uuid,agent_external_id,name,mode,app_id)
VALUES ('chnl_group',$1,$2,'agent_claim','群聊迁移','open','test-app')`, claimTestOrganization, claimTestWorkspace)
	execClaimTest(t, ctx, database, `INSERT INTO channel_sessions (channel_external_id,scope_key,session_external_id) VALUES ('chnl_group','chat:group','sesn_claim_302')`)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	var migratedID string
	if err := database.QueryRowContext(ctx, `SELECT count(*),min(v.external_id) FROM channel_user_vaults p JOIN vaults v ON v.uuid=p.vault_uuid`).Scan(&count, &migratedID); err != nil || count != 1 || migratedID != legacyID {
		t.Fatalf("历史迁移信任了不完整或伪造身份：%d %s %v", count, migratedID, err)
	}
	var personal, group, unbound bool
	if err := database.QueryRowContext(ctx, `SELECT metadata->>'oauth_vault_id'=$1 AND metadata->>'credential_binding_mode'='first_vault' FROM sessions WHERE external_id='sesn_claim_301'`, legacyID).Scan(&personal); err != nil || !personal {
		t.Fatalf("旧个人会话授权目标丢失：%v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT metadata->>'credential_binding_mode'='first_vault' AND vault_ids='["vlt_shared"]'::jsonb AND NOT metadata ? 'personal_vault_id' FROM sessions WHERE external_id='sesn_claim_302'`).Scan(&group); err != nil || !group {
		t.Fatalf("旧群聊未保留共享挂载：%v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT NOT metadata ? 'credential_binding_mode' FROM sessions WHERE external_id='sesn_claim_303'`).Scan(&unbound); err != nil || !unbound {
		t.Fatalf("不应改写无渠道绑定的会话：%v", err)
	}
}

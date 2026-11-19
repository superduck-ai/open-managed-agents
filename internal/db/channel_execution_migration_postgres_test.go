//go:build integration

package db

import (
	"errors"
	"os"
	"testing"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

func TestConsolidateChannelExecutionPostgres(t *testing.T) {
	url := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, url)
	if _, err := migrations.UpTo(ctx, 161); err != nil {
		t.Fatal(err)
	}
	org, workspace, identity := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	execClaimTest(t, ctx, database, `insert into identities
		(uuid,external_id,organization_uuid,workspace_uuid,external_subject_id,display_name)
		values ($1,'idn_test',$2,$3,'actor','固定身份')`, identity, org, workspace)
	for _, id := range []string{"fixed", "paired", "legacy"} {
		execClaimTest(t, ctx, database, `insert into channels
			(external_id,organization_uuid,workspace_uuid,agent_external_id,name,channel_type,mode,mounts)
			values ($1,$2,$3,'agent_test',$1,'feishu','open','{"agent_version":2}')`, id, org, workspace)
	}
	execClaimTest(t, ctx, database, `insert into channel_execution_contexts
		(organization_uuid,workspace_uuid,channel_uuid,channel_external_id,mode,identity_uuid,agent_version,revision,connection_status,entry_closed)
		select organization_uuid,workspace_uuid,uuid,external_id,'fixed',$1,1,4,'healthy',false from channels where external_id='fixed'`, identity)
	execClaimTest(t, ctx, database, `insert into channel_execution_contexts
		(organization_uuid,workspace_uuid,channel_uuid,channel_external_id,mode)
		select organization_uuid,workspace_uuid,uuid,external_id,'pairing' from channels where external_id='paired'`)
	execClaimTest(t, ctx, database, `insert into channel_pairings
		(external_id,channel_external_id,kind,target,status) values ('access','paired','user','ou_actor','approved')`)
	execClaimTest(t, ctx, database, `insert into channel_scope_pairings
		(external_id,organization_uuid,workspace_uuid,channel_uuid,channel_external_id,scope_kind,platform_account,remote_id,status,identity_uuid,agent_external_id,agent_version)
		select 'scope',organization_uuid,workspace_uuid,uuid,external_id,'direct','app','ou_actor','active',$1,'agent_test',3 from channels where external_id='paired'`, identity)

	if _, err := migrations.UpTo(ctx, 164); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	fixed, err := store.FindExecutionContext(ctx, workspace, "fixed")
	if err != nil || fixed.Mode != "fixed" || fixed.IdentityUUID == nil || *fixed.IdentityUUID != identity ||
		fixed.AgentVersion == nil || *fixed.AgentVersion != 1 || fixed.Revision != 4 || fixed.ConnectionStatus != "healthy" {
		t.Fatalf("固定渠道没有保留原执行配置：%+v %v", fixed, err)
	}
	if _, err := store.FindExecutionContext(ctx, workspace, "legacy"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("旧渠道没有保留回退路径：%v", err)
	}
	paired, err := store.FindExecutionContext(ctx, workspace, "paired")
	if err != nil || paired.Mode != "pairing" || paired.AgentVersion == nil || *paired.AgentVersion != 2 {
		t.Fatalf("配对渠道没有保留默认版本：%+v %v", paired, err)
	}
	scope, err := store.FindScopePairing(ctx, workspace, "scope")
	if err != nil || scope.Status != "active" || scope.AgentVersion == nil || *scope.AgentVersion != 3 {
		t.Fatalf("范围配对没有保留独立的版本：%+v %v", scope, err)
	}
	access, err := store.ListChannelPairingRecords(ctx, "paired")
	if err != nil || len(access) != 1 || access[0].Status != "approved" {
		t.Fatalf("丢失渠道准入记录：%+v %v", access, err)
	}
	bindings, err := store.ListAgentIdentityBindings(ctx, workspace, "agent_test")
	if err != nil || len(bindings) != 2 {
		t.Fatalf("Agent 的固定身份和范围绑定应同时可查：%+v %v", bindings, err)
	}
	changed, err := store.UpdateChannelRecord(ctx, ChannelRecordUpdate{
		WorkspaceUUID: workspace, ExternalID: "fixed", Name: "fixed", Enabled: true,
		BindingStatus: "unbound", Mounts: `{"agent_version":2}`,
	})
	if err != nil || changed.ExecutionMode == nil || *changed.ExecutionMode != "fixed" {
		t.Fatalf("仅更新版本失败：%+v %v", changed, err)
	}
	fixed, err = store.FindExecutionContext(ctx, workspace, "fixed")
	if err != nil || fixed.AgentVersion == nil || *fixed.AgentVersion != 2 || fixed.Revision != 5 {
		t.Fatalf("版本没有同步给执行路径或修订号没有递增：%+v %v", fixed, err)
	}
}

//go:build integration

package db

import (
	"errors"
	"os"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestCreateChannelRecordPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	const (
		organization    = "aa000000-0000-4000-8000-0000000000d1"
		workspace       = "aa000000-0000-4000-8000-0000000000d2"
		otherWorkspace  = "aa000000-0000-4000-8000-0000000000d3"
		agentExternalID = "agent_channel_test"
	)
	seed := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO organizations (uuid, name) VALUES ($1, $2)`, []any{organization, "渠道回归"}},
		{`INSERT INTO workspaces (uuid, external_id, organization_uuid, name) VALUES ($1, $2, $3, $4)`, []any{workspace, "ws_channel_test", organization, "渠道回归"}},
		{`INSERT INTO workspaces (uuid, external_id, organization_uuid, name) VALUES ($1, $2, $3, $4)`, []any{otherWorkspace, "ws_channel_other", organization, "其他工作区"}},
		{`INSERT INTO agents (external_id, name, model, workspace_uuid) VALUES ($1, $2, $3, $4)`, []any{agentExternalID, "渠道 Agent", `"test-model"`, workspace}},
	}
	for _, statement := range seed {
		if _, err := database.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	input := ChannelRecordInput{
		EnvironmentID:    new("env_original"),
		ExternalID:       "ch_postgres_test",
		OrganizationUUID: organization,
		WorkspaceUUID:    workspace,
		AgentExternalID:  agentExternalID,
		Name:             "飞书渠道",
		ChannelType:      "feishu",
		Mode:             "open",
		Enabled:          true,
		BindingStatus:    "unbound",
	}
	record, err := store.CreateChannelRecord(ctx, input)
	if err != nil {
		t.Fatalf("创建渠道失败：%v", err)
	}
	if record.ExternalID != input.ExternalID || record.WorkspaceUUID != workspace {
		t.Fatalf("渠道记录 = %+v", record)
	}

	badInput := input
	badInput.ExternalID = "ch_postgres_other"
	badInput.WorkspaceUUID = otherWorkspace
	if _, err := store.CreateChannelRecord(ctx, badInput); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨工作区创建渠道应失败，实际 %v", err)
	}

	loaded, err := store.GetChannelRecordByID(ctx, organization, workspace, input.ExternalID)
	if err != nil || loaded.ExternalID != input.ExternalID {
		t.Fatalf("按 ID 读取渠道 = %+v, %v", loaded, err)
	}
	environment, err := store.GetChannelAgentEnvironment(ctx, organization, workspace, agentExternalID)
	if err != nil || environment != "env_original" {
		t.Fatalf("创建渠道和环境应一并提交: %s %v", environment, err)
	}
	if _, err := database.ExecContext(ctx, `ALTER TABLE channel_agent_settings ADD CONSTRAINT test_environment_write CHECK (environment_external_id <> 'env_rejected')`); err != nil {
		t.Fatal(err)
	}
	update := ChannelRecordUpdate{ExternalID: input.ExternalID, WorkspaceUUID: workspace, Name: "不应提交的名称", Enabled: true, BindingStatus: "unbound", EnvironmentID: new("env_rejected")}
	if _, err := store.UpdateChannelRecord(ctx, update); err == nil {
		t.Fatal("环境保存故障应使整个更新失败")
	}
	after, err := store.GetChannelRecord(ctx, organization, workspace, agentExternalID, input.ExternalID)
	if err != nil || after.Name != input.Name {
		t.Fatalf("渠道更新没有回滚: %+v %v", after, err)
	}
	environment, err = store.GetChannelAgentEnvironment(ctx, organization, workspace, agentExternalID)
	if err != nil || environment != "env_original" {
		t.Fatalf("失败更新改动了原环境: %s %v", environment, err)
	}
	rejected := input
	rejected.ExternalID = "ch_rejected_environment"
	rejected.EnvironmentID = new("env_rejected")
	if _, err := store.CreateChannelRecord(ctx, rejected); err == nil {
		t.Fatal("环境写入失败时创建必须失败")
	}
	if _, err := store.GetChannelRecord(ctx, organization, workspace, agentExternalID, rejected.ExternalID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("失败创建残留了渠道: %v", err)
	}
	update.EnvironmentID = new("env_new")
	update.Name = "已一并保存"
	if _, err := store.UpdateChannelRecord(ctx, update); err != nil {
		t.Fatal(err)
	}
	after, err = store.GetChannelRecord(ctx, organization, workspace, agentExternalID, input.ExternalID)
	if err != nil || after.Name != update.Name {
		t.Fatalf("正常更新未提交: %+v %v", after, err)
	}
	environment, err = store.GetChannelAgentEnvironment(ctx, organization, workspace, agentExternalID)
	if err != nil || environment != "env_new" {
		t.Fatalf("正常更新环境未提交: %s %v", environment, err)
	}

	t.Run("应用绑定冲突", func(t *testing.T) {
		bound := input
		bound.EnvironmentID = nil
		bound.ExternalID = "ch_bound_app"
		bound.Enabled = false
		bound.BindingStatus = "bound"
		bound.AppID = "cli_bound"
		if _, err := store.CreateChannelRecord(ctx, bound); err != nil {
			t.Fatal(err)
		}
		duplicate := bound
		duplicate.ExternalID = "ch_duplicate_app"
		if _, err := store.CreateChannelRecord(ctx, duplicate); !errors.Is(err, ErrChannelAppAlreadyBound) {
			t.Fatalf("重复绑定已停用渠道的应用应返回绑定冲突，实际 %v", err)
		}
		update.BindingStatus = "bound"
		update.AppID = bound.AppID
		if _, err := store.UpdateChannelRecord(ctx, update); !errors.Is(err, ErrChannelAppAlreadyBound) {
			t.Fatalf("改绑已占用的应用应返回绑定冲突，实际 %v", err)
		}
	})

	t.Run("卡片回执与恢复许可", func(t *testing.T) {
		testChannelReplyDeliveriesPostgres(t, store, database, ChannelDeliveryScope{OrganizationUUID: organization, WorkspaceUUID: workspace, ChannelExternalID: input.ExternalID}, otherWorkspace)
	})

}

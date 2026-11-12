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
		{`INSERT INTO agents (external_id, name, model, workspace_uuid) VALUES ($1, $2, $3, $4)`, []any{agentExternalID, "渠道 Agent", "test-model", workspace}},
	}
	for _, statement := range seed {
		if _, err := database.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	input := ChannelRecordInput{
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
}

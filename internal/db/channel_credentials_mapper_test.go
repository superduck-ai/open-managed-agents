package db

import (
	"database/sql/driver"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestChannelCredentialMapper(t *testing.T) {
	t.Run("跨工作区或不存在时没有记录", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"external_id", "envelope", "status"}})
		_, found, err := NewChannelCredentialMapper(executor).Find(t.Context(), "org", "other")
		if err != nil || found {
			t.Fatalf("Find = %v, %v", found, err)
		}
		assertMapperTestExecution(t, executor, "ChannelCredentialMapper.Find", yourbatis.StatementSelect, []any{"org", "other"}, "c.organization_uuid = $1 AND c.workspace_uuid = $2", "k.workspace_uuid = c.workspace_uuid")
	})
	t.Run("锁定有效工作区", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"external_id"}, rows: [][]driver.Value{{"wrkspc_a"}}})
		id, err := NewChannelCredentialMapper(executor).LockWorkspace(t.Context(), "org", "workspace")
		if err != nil || id != "wrkspc_a" {
			t.Fatalf("LockWorkspace = %s, %v", id, err)
		}
		assertMapperTestExecution(t, executor, "ChannelCredentialMapper.LockWorkspace", yourbatis.StatementSelect, []any{"org", "workspace"}, "organization_uuid = $1 AND uuid = $2", "archived_at IS NULL FOR UPDATE")
	})
	t.Run("读取密文及停用状态", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"external_id", "envelope", "status"}, rows: [][]driver.Value{{"apikey_a", "{}", "inactive"}}})
		row, found, err := NewChannelCredentialMapper(executor).Find(t.Context(), "org", "workspace")
		if err != nil || !found || row.Status != "inactive" || row.Envelope != "{}" {
			t.Fatalf("Find = %+v, %v, %v", row, found, err)
		}
	})
	t.Run("写入密文参数标记敏感", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{})
		err := NewChannelCredentialMapper(executor).Insert(t.Context(), insertChannelCredentialParams{ExternalID: "apikey_a", OrganizationUUID: "org", WorkspaceUUID: "workspace", APIKeyUUID: "key-uuid", Envelope: "encrypted"})
		if err != nil {
			t.Fatal(err)
		}
		assertMapperBuilderContract(t, mapperBuilderContract{statement: executor.statement, bound: executor.bound, wantID: "ChannelCredentialMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{"params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID", "params.APIKeyUUID", "params.Envelope"}, wantSensitiveArgumentNames: []string{"params.Envelope"}, wantSQLFragments: []string{"INSERT INTO channel_workspace_credentials"}})
	})
}

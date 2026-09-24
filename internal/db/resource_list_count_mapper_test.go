package db

import (
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestResourceListCountsIgnorePageCursor(t *testing.T) {
	now := time.Date(2026, time.September, 24, 3, 0, 0, 0, time.UTC)
	cursorID := "00000000-0000-4000-8000-000000000099"
	cases := []struct {
		name string
		sql  string
		want []string
		skip []string
	}{
		{
			name: "sessions",
			sql: buildSessionMapperCountList(yourbatis.DialectPostgres, sessionPageMapperParams{
				WorkspaceUUID: "ws", IncludeArchived: false, AgentExternalID: "agent_1",
				Statuses: []string{"idle"}, Cursor: &SessionPageCursor{CreatedAt: now, UUID: cursorID},
			}).SQL,
			want: []string{"COUNT(*)", "FROM sessions", "agent_external_id", "status IN"},
			skip: []string{"LIMIT", "(s.created_at, s.uuid)"},
		},
		{
			name: "agents",
			sql: buildAgentMapperCountList(yourbatis.DialectPostgres, agentPageFilter{
				WorkspaceUUID: "ws", Name: "alpha", IncludeArchived: false,
				Cursor: &AgentPageCursor{CreatedAt: now, UUID: cursorID},
			}).SQL,
			want: []string{"COUNT(*)", "FROM agents", "POSITION"},
			skip: []string{"LIMIT", "(created_at, uuid)"},
		},
		{
			name: "deployments",
			sql: buildDeploymentMapperCountList(yourbatis.DialectPostgres, deploymentPageMapperParams{
				WorkspaceUUID: "ws", AgentExternalID: "agent_1", Status: "active", IncludeArchived: false,
				Cursor: &DeploymentPageCursor{CreatedAt: now, UUID: cursorID},
			}).SQL,
			want: []string{"COUNT(*)", "FROM deployments", "agent_external_id", "status ="},
			skip: []string{"LIMIT", "(created_at, uuid)"},
		},
		{
			name: "environments",
			sql: buildEnvironmentMapperCountList(yourbatis.DialectPostgres, environmentPageMapperParams{
				WorkspaceUUID: "ws", IncludeArchived: false,
				Cursor: &EnvironmentPageCursor{CreatedAt: now, UUID: cursorID},
			}).SQL,
			want: []string{"COUNT(*)", "FROM environments", "archived_at IS NULL"},
			skip: []string{"LIMIT", "(created_at, uuid)"},
		},
		{
			name: "vaults",
			sql: buildVaultMapperCountList(yourbatis.DialectPostgres, listVaultsMapperParams{
				WorkspaceUUID: "ws", IncludeArchived: true,
				Cursor: &VaultPageCursor{CreatedAt: now, UUID: cursorID},
			}).SQL,
			want: []string{"COUNT(*)", "FROM vaults"},
			skip: []string{"LIMIT", "archived_at IS NULL", "(created_at, uuid)"},
		},
		{
			name: "memory stores",
			sql: buildMemoryStoreMapperCountList(yourbatis.DialectPostgres, listMemoryStoresParams{
				WorkspaceUUID: "ws", IncludeArchived: false, HasCreatedAtGTE: true, CreatedAtGTE: now,
				HasCursor: true, CursorCreatedAt: now, CursorUUID: cursorID,
			}).SQL,
			want: []string{"COUNT(*)", "FROM memory_stores", "created_at >="},
			skip: []string{"LIMIT", "uuid <"},
		},
		{
			name: "skills",
			sql:  buildSkillMapperCountByWorkspace(yourbatis.DialectPostgres, "ws").SQL,
			want: []string{"COUNT(*)", "FROM skills", "deleted_at IS NULL"},
			skip: []string{"LIMIT", "OFFSET"},
		},
		{
			name: "batches",
			sql:  buildMessageBatchMapperCountByWorkspace(yourbatis.DialectPostgres, "ws").SQL,
			want: []string{"COUNT(*)", "FROM message_batches", "deleted_at IS NULL"},
			skip: []string{"LIMIT"},
		},
		{
			name: "files",
			sql: buildFileMapperCountFiles(yourbatis.DialectPostgres, fileMapperListParams{
				WorkspaceUUID: "ws", HasScope: true, ScopeID: "scope_1", HasCursor: true, CursorUUID: cursorID,
			}).SQL,
			want: []string{"COUNT(*)", "FROM files", "scope_id"},
			skip: []string{"LIMIT", "uuid <"},
		},
		{
			name: "session files",
			sql: buildFileMapperCountSessionFiles(yourbatis.DialectPostgres, fileMapperListParams{
				WorkspaceUUID: "ws", ScopeID: "sesn_1", HasCursor: true, CursorUUID: cursorID,
			}).SQL,
			want: []string{"COUNT(*)", "catalog_resource", "session_external_id"},
			skip: []string{"LIMIT", "uuid <"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			for _, fragment := range test.want {
				if !strings.Contains(test.sql, fragment) {
					t.Fatalf("count SQL missing %q: %s", fragment, test.sql)
				}
			}
			for _, fragment := range test.skip {
				if strings.Contains(test.sql, fragment) {
					t.Fatalf("count SQL includes %q: %s", fragment, test.sql)
				}
			}
		})
	}
}

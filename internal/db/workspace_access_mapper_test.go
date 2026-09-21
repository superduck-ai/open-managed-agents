package db

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestWorkspaceAccessMapperBindings(t *testing.T) {
	const org = "aa000000-0000-4000-8000-000000000001"
	const workspace = "aa000000-0000-4000-8000-000000000002"
	t.Run("用户显式角色批量绑定和扫描", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"workspace_uuid", "workspace_role"}, rows: [][]driver.Value{{workspace, "workspace_admin"}},
		})
		facts, err := NewWorkspaceAccessMapper(executor).ListUserRoles(t.Context(), org, "aa000000-0000-4000-8000-000000000003")
		if err != nil || len(facts) != 1 || facts[0].WorkspaceUUID != workspace || facts[0].Role != "workspace_admin" {
			t.Fatalf("facts = %+v, err = %v", facts, err)
		}
		assertMapperTestExecution(t, executor, "WorkspaceAccessMapper.ListUserRoles", yourbatis.StatementSelect,
			[]any{org, "aa000000-0000-4000-8000-000000000003"}, "wm.organization_uuid = $1 AND wm.user_uuid = $2", "wm.deleted_at IS NULL", "NOT w.is_default", "w.archived_at IS NULL")
	})
	t.Run("成员投影绑定和扫描", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"user_uuid", "user_external_id", "organization_role", "explicit_role"},
			rows:    [][]driver.Value{{"aa000000-0000-4000-8000-000000000003", "user_member", "billing", "workspace_admin"}},
		})
		facts, err := NewWorkspaceAccessMapper(executor).ListMemberFacts(context.Background(), org, workspace)
		if err != nil || len(facts) != 1 || facts[0].ExplicitRole != "workspace_admin" {
			t.Fatalf("facts = %+v, err = %v", facts, err)
		}
		assertMapperTestExecution(t, executor, "WorkspaceAccessMapper.ListMemberFacts", yourbatis.StatementSelect,
			[]any{workspace, org}, "LEFT JOIN workspace_members wm ON NOT w.is_default", "u.deleted_at IS NULL")
	})
	t.Run("空间锁绑定", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}, rows: [][]driver.Value{{workspace}}})
		result, err := NewWorkspaceAccessMapper(executor).LockWorkspace(context.Background(), org, workspace)
		if err != nil || result != workspace {
			t.Fatalf("workspace = %s, err = %v", result, err)
		}
		assertMapperTestExecution(t, executor, "WorkspaceAccessMapper.LockWorkspace", yourbatis.StatementSelect,
			[]any{org, workspace}, "organization_uuid = $1 AND uuid = $2", "FOR UPDATE")
	})
	t.Run("组织成员锁绑定", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "external_id", "organization_uuid", "email", "name", "role", "added_at"}})
		_, err := NewWorkspaceAccessMapper(executor).LockUsers(context.Background(), org, "user_actor", "user_target")
		if err != nil {
			t.Fatal(err)
		}
		assertMapperTestExecution(t, executor, "WorkspaceAccessMapper.LockUsers", yourbatis.StatementSelect,
			[]any{org, "user_actor", "user_target"}, "external_id IN ($2, $3)", "ORDER BY uuid FOR UPDATE")
	})
}

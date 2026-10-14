package db

import (
	"context"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestTerminateCodeSessionsBySessionContract(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         codeSessionMapperTerminateBySessionStatement,
		bound:             buildCodeSessionMapperTerminateBySession(yourbatis.DialectPostgres, "org", "workspace", "session"),
		wantID:            "CodeSessionMapper.TerminateBySession",
		wantKind:          yourbatis.StatementUpdate,
		wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "sessionUUID"},
		wantSQLFragments: []string{
			"current_worker_epoch = current_worker_epoch + 1", "oauth_access_token_hash = NULL",
			"worker_lease_expires_at = NULL", "organization_uuid = $1", "workspace_uuid = $2",
			"session_uuid = $3", "RETURNING external_id",
		},
	})
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"external_id"}})
	ids, err := NewCodeSessionMapper(executor).TerminateBySession(context.Background(), "org", "workspace", "session")
	if err != nil || len(ids) != 0 {
		t.Fatalf("missing code sessions: %v %v", ids, err)
	}
	assertMapperTestExecution(t, executor, "CodeSessionMapper.TerminateBySession", yourbatis.StatementUpdate, []any{"org", "workspace", "session"})
}

func TestRetiredSessionIncludesDeletedRowsAndTenantScope(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sessionMapperIsRetiredStatement,
		bound:     buildSessionMapperIsRetired(yourbatis.DialectPostgres, "org", "workspace", "session"),
		wantID:    "SessionMapper.IsRetired", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "sessionUUID"},
		wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2", "uuid = $3", "archived_at IS NOT NULL", "deleted_at IS NOT NULL", "status = 'terminated'"},
	})
}

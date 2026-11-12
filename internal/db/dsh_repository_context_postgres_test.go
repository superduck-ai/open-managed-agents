//go:build integration

package db

import (
	"os"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	"github.com/superduck-ai/yourbatis"
)

func TestDSHRepositoryCatalogFreezesBeforeWorkerDispatchPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	id := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	record, found, err := store.GetCodeSession(ctx, id)
	if err != nil || !found || record.WorkerRegisteredAt != nil {
		t.Fatal("fixture must precede registration", err)
	}
	scope := DSHScope{CodeSessionID: id, SessionID: record.SessionExternalID, OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID, CodeSessionUUID: record.UUID, Epoch: record.CurrentWorkerEpoch}
	candidate := []byte(`{"model":"model","cwd":"/repo","config":{"runtime_skills":[{"name":"repo-skill","description":"frozen A","path":"/repo/.claude/skills/repo-skill/SKILL.md"}]}}`)
	frozen, err := store.FreezeDSHContext(ctx, scope, candidate)
	same, compareErr := jsoncanonical.Equal(frozen, candidate)
	if err != nil || compareErr != nil || !same {
		t.Fatal("pre-dispatch freeze failed", err)
	}
	if _, err = store.RegisterDSHWorker(ctx, scope); err != nil {
		t.Fatal(err)
	}
	after, err := store.FreezeDSHContext(ctx, scope, []byte(`{"config":{"runtime_skills":[]}}`))
	if err != nil || string(after) != string(frozen) {
		t.Fatal("registration rescanned or replaced catalogue", err)
	}
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET current_worker_epoch=current_worker_epoch+1 WHERE external_id=$1`, id)
	scope.Epoch++
	if _, err = store.RegisterDSHWorker(ctx, scope); err != nil {
		t.Fatal(err)
	}
	after, err = store.GetDSHContextSnapshot(ctx, scope)
	if err != nil || string(after) != string(frozen) {
		t.Fatal("cold owner changed frozen catalogue", err)
	}
}

func TestDSHRepositoryCatalogBlocksOnlyUnpreparedRepositoryClaimsPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	for index, test := range []struct {
		name, tools, sources string
		frozen, want         bool
	}{
		{"read-repository-before-freeze", `["Read"]`, `[{"type":"git_repository","mount_path":"/repo"}]`, false, false},
		{"read-repository-after-freeze", `["Read"]`, `[{"type":"git_repository","mount_path":"/repo"}]`, true, true},
		{"read-disabled-repository", `["Bash"]`, `[{"type":"git_repository","mount_path":"/repo"}]`, false, true},
		{"attached-only", `["Read"]`, `[]`, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := seedClaimCodeSession(t, ctx, database, index+1, "claim", "running")
			execClaimTest(t, ctx, database, `UPDATE code_sessions SET metadata=jsonb_build_object('config',jsonb_build_object('worker_transport','claim','worker_protocol','dsr1','runtime_tools',$2::jsonb,'sources',$3::jsonb)) WHERE external_id=$1`, id, test.tools, test.sources)
			if err := store.RequestCodeSessionWorker(ctx, claimTestOrganization, claimTestWorkspace, id, ""); err != nil {
				t.Fatal(err)
			}
			if test.frozen {
				code, found, err := store.GetCodeSession(ctx, id)
				if err != nil || !found {
					t.Fatal(err)
				}
				scope := DSHScope{OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, CodeSessionUUID: code.UUID, Epoch: code.CurrentWorkerEpoch}
				if _, err = store.FreezeDSHContext(ctx, scope, []byte(`{"config":{"runtime_skills":[]}}`)); err != nil {
					t.Fatal(err)
				}
			}
			input := claimTestInput(test.name)
			input.WorkerProtocol = "dsr1"
			claim, found, err := store.ClaimNextCodeSession(ctx, input)
			if err != nil || found != test.want || (found && claim.CodeSessionExternalID != id) {
				t.Fatal("claim readiness mismatch", err, found, claim)
			}
			execClaimTest(t, ctx, database, `UPDATE code_sessions SET status='terminated' WHERE external_id=$1`, id)
		})
	}
}

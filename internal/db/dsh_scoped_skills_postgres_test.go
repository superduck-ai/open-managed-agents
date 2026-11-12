//go:build integration

package db

import (
	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	"testing"
)

func TestDSHScopedSkillCatalogueSurvivesOwnerReplacementPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	id := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	record, found, err := store.GetCodeSession(ctx, id)
	if err != nil || !found {
		t.Fatal(err)
	}
	scope := DSHScope{CodeSessionID: id, SessionID: record.SessionExternalID, OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID, CodeSessionUUID: record.UUID, Epoch: record.CurrentWorkerEpoch}
	candidate := []byte(`{"model":"model","cwd":"/home/user","config":{"skill_catalog_version":1,"runtime_skills":[{"name":"demo","version":"B","path":"/root/.claude/skills/version-B/SKILL.md","requested_versions":["latest"]}],"agent_skills":{"worker":{"agent_id":"agent_worker","agent_version":1,"skills":[{"name":"demo","version":"A","path":"/root/.claude/skills/version-A/SKILL.md","requested_versions":["A"]}]}}}}`)
	frozen, err := store.FreezeDSHContext(ctx, scope, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if same, err := jsoncanonical.Equal(frozen, candidate); err != nil || !same {
		t.Fatal("freeze lost scoped bindings", err)
	}
	if _, err := store.RegisterDSHWorker(ctx, scope); err != nil {
		t.Fatal(err)
	}
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET current_worker_epoch=current_worker_epoch+1,metadata=jsonb_build_object('config',jsonb_build_object('runtime_skills',jsonb_build_array(jsonb_build_object('version','C')))) WHERE external_id=$1`, id)
	scope.Epoch++
	if _, err := store.RegisterDSHWorker(ctx, scope); err != nil {
		t.Fatal(err)
	}
	after, err := store.FreezeDSHContext(ctx, scope, []byte(`{"config":{"skill_catalog_version":1,"runtime_skills":[{"version":"C"}],"agent_skills":{}}}`))
	if err != nil || string(after) != string(frozen) {
		t.Fatal("cold owner used new latest/config instead of frozen scoped plan", err)
	}
}

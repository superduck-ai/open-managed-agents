//go:build integration

package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestDeletedSessionSandboxDurableReclamationPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	database, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tx, err := database.mapperDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TEMP TABLE sessions(uuid uuid PRIMARY KEY,external_id text,organization_uuid uuid,workspace_uuid uuid,environment_uuid uuid,archived_at timestamptz,deleted_at timestamptz) ON COMMIT DROP`,
		`CREATE TEMP TABLE environments(uuid uuid PRIMARY KEY,organization_uuid uuid,workspace_uuid uuid,config jsonb) ON COMMIT DROP`,
		`CREATE TEMP TABLE environment_work(uuid uuid PRIMARY KEY,organization_uuid uuid,workspace_uuid uuid,environment_uuid uuid,session_uuid uuid,state text,deleted_at timestamptz,stop_requested_at timestamptz,stopped_at timestamptz,updated_at timestamptz) ON COMMIT DROP`,
		`CREATE TEMP TABLE environment_sandboxes(uuid uuid PRIMARY KEY,organization_uuid uuid,workspace_uuid uuid,environment_uuid uuid,work_uuid uuid,provider_sandbox_id text,state text,stop_reason text,stopped_at timestamptz,updated_at timestamptz) ON COMMIT DROP`,
		`CREATE TEMP TABLE code_sessions(uuid uuid,external_id text,organization_uuid uuid,workspace_uuid uuid,environment_uuid uuid,session_uuid uuid,created_at timestamptz,status text,deleted_at timestamptz,worker_status text,idle_since timestamptz) ON COMMIT DROP`,
	} {
		execMapperFixtureSQL(t, ctx, tx, statement)
	}
	org, workspace, env := "88000000-0000-0000-0000-000000000001", "88000000-0000-0000-0000-000000000002", "88000000-0000-0000-0000-000000000003"
	removed, active, archived := "88000000-0000-0000-0000-000000000011", "88000000-0000-0000-0000-000000000012", "88000000-0000-0000-0000-000000000013"
	sandbox := "88000000-0000-0000-0000-000000000021"
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO environments VALUES($1,$2,$3,'{"type":"cloud"}')`, env, org, workspace)
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO sessions VALUES($1,'deleted',$4,$5,$6,NULL,NOW()),($2,'active',$4,$5,$6,NULL,NULL),($3,'archived',$4,$5,$6,NOW(),NULL)`, removed, active, archived, org, workspace, env)
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO environment_work(uuid,organization_uuid,workspace_uuid,environment_uuid,session_uuid,state) SELECT uuid,organization_uuid,workspace_uuid,environment_uuid,uuid,'active' FROM sessions`)
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO environment_sandboxes(uuid,organization_uuid,workspace_uuid,environment_uuid,work_uuid,provider_sandbox_id,state) VALUES($1,$2,$3,$4,$5,'exact-old-provider','running'),('88000000-0000-0000-0000-000000000022',$2,$3,$4,$6,'active-provider','running'),('88000000-0000-0000-0000-000000000023',$2,$3,$4,$7,'archived-provider','running')`, sandbox, org, workspace, env, removed, active, archived)
	mapper := NewSandboxLifecycleMapper(tx)
	if err := mapper.BeginRemovalStop(ctx, org, workspace, removed); err != nil {
		t.Fatal(err)
	}
	targets, err := mapper.ListRemovedForSession(ctx, org, workspace, removed)
	if err != nil || len(targets) != 1 || targets[0].ProviderSandboxID != "exact-old-provider" || !targets[0].reclaimTarget().Reclaiming {
		t.Fatalf("deleted Session did not durably claim its immutable provider: %+v %v", targets, err)
	}
	if targets[0].StopReason == nil || *targets[0].StopReason != "session_deleted" {
		t.Fatalf("deleted stop reason lost: %+v", targets[0])
	}
	if err := mapper.BeginRemovalStop(ctx, org, workspace, removed); err != nil {
		t.Fatal(err)
	}
	candidates, err := mapper.ListCandidates(ctx, time.Now(), "", 10, false)
	if err != nil || len(candidates) != 1 || candidates[0].ProviderSandboxID != "exact-old-provider" {
		t.Fatalf("durable deleted target is not retryable without new idle claims: %+v %v", candidates, err)
	}
	scope := sandboxLifecycleScope{OrganizationUUID: org, WorkspaceUUID: workspace, SandboxUUID: sandbox}
	count, err := mapper.FinishStop(ctx, scope)
	if err != nil || count != 1 {
		t.Fatalf("provider-success completion failed: %d %v", count, err)
	}
	count, err = mapper.FinishStop(ctx, scope)
	if err != nil || count != 0 {
		t.Fatalf("completion not idempotent: %d %v", count, err)
	}
	candidates, err = mapper.ListCandidates(ctx, time.Now(), "", 10, false)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("completed/active foreign targets selected: %+v %v", candidates, err)
	}
	if err := mapper.BeginRemovalStop(ctx, org, workspace, archived); err != nil {
		t.Fatal(err)
	}
	targets, err = mapper.ListRemovedForSession(ctx, org, workspace, archived)
	if err != nil || len(targets) != 1 || targets[0].StopReason == nil || *targets[0].StopReason != "session_archived" {
		t.Fatalf("archive regression: %+v %v", targets, err)
	}
}

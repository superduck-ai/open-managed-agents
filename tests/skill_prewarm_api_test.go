package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSkillPrewarmAPIEnqueuesJobs(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("skill-prewarm-api-bucket"))
	defer app.close()

	skill := createSkill(t, app, "prewarm-skill")
	defer deleteSkill(t, app, skill.ID)

	agent := createAgent(t, app, `{
		"model":"claude-opus-4-6",
		"name":"prewarm-agent",
		"skills":[{"type":"custom","skill_id":`+quoteJSON(skill.ID)+`}]
	}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	assertSkillPrewarmJobCount(t, app, `payload->>'kind' = 'snapshot' and payload->>'source' = 'agent' and payload->>'source_id' = $1 and payload->>'trigger' = 'agent_create'`, agent.ID, 1)

	env := createEnvironment(t, app, `{"name":"prewarm-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	deployment := createDeployment(t, app, minimalDeploymentBody(agent.ID, env.ID))
	defer cleanupDeploymentRows(t, app, deployment.ID)
	assertSkillPrewarmJobCount(t, app, `payload->>'kind' = 'snapshot' and payload->>'source' = 'deployment' and payload->>'source_id' = $1 and payload->>'trigger' = 'deployment_create'`, deployment.ID, 1)

	updatedAgent := updateAgent(t, app, agent.ID, `{"version":1,"name":"prewarm-agent-renamed"}`, http.StatusOK)
	if updatedAgent.Name != "prewarm-agent-renamed" {
		t.Fatalf("updated agent name = %q, want prewarm-agent-renamed", updatedAgent.Name)
	}
	assertSkillPrewarmJobCount(t, app, `payload->>'kind' = 'snapshot' and payload->>'source' = 'agent' and payload->>'source_id' = $1 and payload->>'trigger' = 'agent_update'`, agent.ID, 0)

	updatedDeployment := updateDeployment(t, app, deployment.ID, `{"name":"prewarm-deployment-renamed"}`)
	if updatedDeployment.Name != "prewarm-deployment-renamed" {
		t.Fatalf("updated deployment name = %q, want prewarm-deployment-renamed", updatedDeployment.Name)
	}
	assertSkillPrewarmJobCount(t, app, `payload->>'kind' = 'snapshot' and payload->>'source' = 'deployment' and payload->>'source_id' = $1 and payload->>'trigger' = 'deployment_update'`, deployment.ID, 0)

	time.Sleep(time.Millisecond)
	body, contentType := skillMultipartBody(t, "", []skillUploadFile{
		{FieldName: "files", Filename: "prewarm-skill/SKILL.md", Content: "---\nname: Prewarm Skill v2\ndescription: v2\n---\n\n# Prewarm Skill v2"},
	})
	resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills/"+skill.ID+"/versions?beta=true", body, defaultTestKey, true, contentType)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create skill version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	assertSkillPrewarmJobCount(t, app, `payload->>'kind' = 'fanout' and payload->>'skill_id' = $1`, skill.ID, 1)
}

func TestSkillPrewarmJobFailureStoresLastErrorAt(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("skill-prewarm-failure-bucket"))
	defer app.close()

	ctx := context.Background()
	ids := getDefaultDBIDs(t, app.db)
	sourceID := "agent_prewarm_failure_" + time.Now().Format("150405.000000000")
	if err := app.db.EnqueueSkillPrewarmSnapshotJob(ctx, db.SkillPrewarmSnapshotJobInput{
		WorkspaceID:   ids.WorkspaceID,
		AgentSnapshot: json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_missing","version":"latest"}]}`),
		Source:        "agent",
		SourceID:      sourceID,
		Trigger:       "test_failure",
	}); err != nil {
		t.Fatalf("enqueue skill prewarm snapshot job: %v", err)
	}
	defer app.db.Pool.Exec(ctx, `delete from jobs where type = 'skill_prewarm' and payload->>'source_id' = $1`, sourceID)

	jobs, err := app.db.LeaseSkillPrewarmJobs(ctx, "skill-prewarm-failure-test", 1, time.Minute)
	if err != nil {
		t.Fatalf("lease skill prewarm jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].WorkspaceID != ids.WorkspaceID {
		t.Fatalf("leased jobs = %+v, want one default workspace job", jobs)
	}
	if err := app.db.FailSkillPrewarmJob(ctx, jobs[0].ID, jobs[0].Attempts, "boom", time.Minute, 5); err != nil {
		t.Fatalf("fail skill prewarm job: %v", err)
	}

	var status, lastError, lastErrorAt string
	var attempts int
	if err := app.db.Pool.QueryRow(ctx, `
		select status, attempts, payload->>'last_error', payload->>'last_error_at'
		from jobs
		where id = $1
	`, jobs[0].ID).Scan(&status, &attempts, &lastError, &lastErrorAt); err != nil {
		t.Fatalf("load failed skill prewarm job: %v", err)
	}
	if status != "retry" || attempts != 1 || lastError != "boom" {
		t.Fatalf("failed job status=%q attempts=%d last_error=%q", status, attempts, lastError)
	}
	if _, err := time.Parse(time.RFC3339Nano, lastErrorAt); err != nil {
		t.Fatalf("last_error_at = %q, want RFC3339Nano timestamp: %v", lastErrorAt, err)
	}
}

func assertSkillPrewarmJobCount(t *testing.T, app *testApp, predicate string, arg string, want int) {
	t.Helper()
	var count int
	query := `select count(*) from jobs where type = 'skill_prewarm' and ` + predicate
	if err := app.db.Pool.QueryRow(context.Background(), query, arg).Scan(&count); err != nil {
		t.Fatalf("count skill prewarm jobs: %v", err)
	}
	if count != want {
		t.Fatalf("skill prewarm job count = %d, want %d for %s", count, want, predicate)
	}
}

package tests

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type deploymentVerification struct {
	app           *testApp
	agentID       string
	environmentID string
	workspaceUUID string
	started       time.Time
}

func requireDeploymentVerification(t *testing.T) {
	t.Helper()
	if os.Getenv("VERIFY_BE_DEPLOYMENT") != "1" {
		t.Skip("run through verify-be deployment with disposable dependencies")
	}
	if !strings.HasPrefix(os.Getenv("VERIFY_BE_RUN_ID"), "verify-be-") || os.Getenv("CONFIG_FILE") == "" {
		t.Fatal("isolated verification configuration required")
	}
}

func newDeploymentVerification(t *testing.T) *deploymentVerification {
	t.Helper()
	requireDeploymentVerification(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.S3.Bucket != "verify-be" {
		t.Fatal("refusing non-verification storage")
	}
	client, err := storage.New(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := client.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		t.Fatal(err)
	}
	app := newPayloadIntegrationApp(t, objects)
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployment-verification"}`)
	environment := createEnvironment(t, app, `{"name":"deployment-verification"}`)
	return &deploymentVerification{app: app, agentID: agent.ID, environmentID: environment.ID, workspaceUUID: getDefaultDBIDs(t, app.pool).WorkspaceUUID, started: time.Now()}
}

func (f *deploymentVerification) create(t *testing.T) deploymentAPIResponse {
	t.Helper()
	created := createDeployment(t, f.app, deploymentBodyWithExtra(f.agentID, f.environmentID, `"schedule":{"type":"cron","expression":"0 0 1 1 *","timezone":"UTC"},"metadata":{"proof":"deployment-verify"}`))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.app.deploymentJobs.DurablePeriodicJobDelete(ctx, created.ID); err != nil && !errors.Is(err, rivertype.ErrNotFound) {
			t.Error(err)
		}
	})
	return created
}

func (f *deploymentVerification) insert(t *testing.T, id string, at time.Time, attempts int) *rivertype.JobRow {
	t.Helper()
	return f.insertSchedule(t, id, at, attempts, deploymentjobs.Schedule{Type: "cron", Expression: "0 0 1 1 *", Timezone: "UTC"})
}

func (f *deploymentVerification) insertSchedule(t *testing.T, id string, at time.Time, attempts int, schedule deploymentjobs.Schedule) *rivertype.JobRow {
	t.Helper()
	result, err := f.app.deploymentJobs.Insert(t.Context(), deploymentjobs.Args{WorkspaceUUID: f.workspaceUUID, DeploymentExternalID: id, Schedule: schedule}, &river.InsertOpts{Queue: deploymentjobs.Queue, ScheduledAt: at, MaxAttempts: attempts})
	if err != nil {
		t.Fatal(err)
	}
	return result.Job
}

func deploymentWait(t *testing.T, label string, timeout time.Duration, check func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if check() {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("waiting for %s: %v", label, ctx.Err())
		}
	}
}

func (f *deploymentVerification) job(t *testing.T, id int64) *rivertype.JobRow {
	t.Helper()
	job, err := f.app.deploymentJobs.JobGet(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func (f *deploymentVerification) waitJob(t *testing.T, id int64, state rivertype.JobState) *rivertype.JobRow {
	t.Helper()
	var job *rivertype.JobRow
	deploymentWait(t, "River job "+string(state), 70*time.Second, func() bool {
		job = f.job(t, id)
		return job.State == state
	})
	if job.FinalizedAt == nil && (state == rivertype.JobStateCompleted || state == rivertype.JobStateDiscarded || state == rivertype.JobStateCancelled) {
		t.Fatal("terminal job has no finalized_at")
	}
	return job
}

func (f *deploymentVerification) runs(t *testing.T, id string, count int) []db.DeploymentRun {
	t.Helper()
	runs, more, err := f.app.db.ListDeploymentRunsPage(t.Context(), db.ListDeploymentRunsPageParams{WorkspaceUUID: f.workspaceUUID, DeploymentExternalID: id, Limit: 20})
	if err != nil || more || len(runs) != count {
		t.Fatalf("runs=%d more=%t want=%d err=%v", len(runs), more, count, err)
	}
	deployment, err := f.app.db.GetDeployment(t.Context(), f.workspaceUUID, id)
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 && deployment.LastRunAt != nil {
		t.Fatal("Run rollback changed last_run_at")
	}
	if count > 0 && (deployment.LastRunAt == nil || !deployment.LastRunAt.Equal(runs[0].CreatedAt)) {
		t.Fatal("last_run_at differs from latest Run")
	}
	return runs
}

func (f *deploymentVerification) effects(t *testing.T, id string, sessions int) {
	t.Helper()
	queries := []string{
		"select count(*) from sessions where deployment_uuid=(select uuid from deployments where external_id=$1)",
		"select count(*) from session_threads where session_uuid in (select uuid from sessions where deployment_uuid=(select uuid from deployments where external_id=$1))",
		"select count(*) from environment_work where session_uuid in (select uuid from sessions where deployment_uuid=(select uuid from deployments where external_id=$1))",
		"select count(*) from session_events where session_uuid in (select uuid from sessions where deployment_uuid=(select uuid from deployments where external_id=$1))",
		"select count(*) from filestore_filesystems where session_uuid in (select uuid from sessions where deployment_uuid=(select uuid from deployments where external_id=$1))",
	}
	for _, query := range queries {
		assertPayloadSQLCount(t, f.app, query, sessions, id)
	}
}

func (f *deploymentVerification) success(t *testing.T, id string, count int) []db.DeploymentRun {
	t.Helper()
	runs := f.runs(t, id, count)
	f.effects(t, id, count)
	deployment, err := f.app.db.GetDeployment(t.Context(), f.workspaceUUID, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		if run.SessionExternalID == nil || len(run.Error) != 0 {
			t.Fatalf("unexpected successful run: %+v", run)
		}
		if run.WorkspaceUUID != deployment.WorkspaceUUID || run.OrganizationUUID != deployment.OrganizationUUID || run.AgentUUID != deployment.AgentUUID || run.AgentVersion != deployment.AgentVersion || !bytes.Equal(run.AgentSnapshot, deployment.AgentSnapshot) {
			t.Fatal("Run lost tenant or Agent snapshot")
		}
		stored, found, err := f.app.db.GetSession(t.Context(), f.workspaceUUID, *run.SessionExternalID)
		if err != nil || !found || stored.AgentUUID != deployment.AgentUUID || stored.AgentVersion != deployment.AgentVersion || stored.RuntimeUserUUID != deployment.RuntimeUserUUID || stored.OrganizationUUID != deployment.OrganizationUUID || !bytes.Equal(stored.AgentSnapshot, deployment.AgentSnapshot) || !bytes.Equal(stored.Metadata, deployment.Metadata) {
			t.Fatalf("Session snapshot or identity differs from Deployment: %v", err)
		}
		public := retrieveDeploymentRun(t, f.app, run.ExternalID)
		if public.SessionID == nil || *public.SessionID != *run.SessionExternalID || string(public.Error) != "null" {
			t.Fatal("HTTP and stored run disagree")
		}
		session := retrieveSession(t, f.app, *run.SessionExternalID, defaultTestKey)
		if session.DeploymentID == nil || *session.DeploymentID != id || session.EnvironmentID != f.environmentID {
			t.Fatal("Session references differ from Deployment")
		}
		workType, sessionID, state := sessionWorkData(t, f.app, session.ID)
		if workType != "session" || sessionID != session.ID || state != "queued" {
			t.Fatal("Run did not enqueue exactly one Session work")
		}
		events := listSessionEvents(t, f.app, session.ID, "", defaultTestKey)
		if len(events.Data) != 1 || !strings.Contains(string(events.Data[0]), `"hello"`) {
			t.Fatal("initial event differs from Deployment")
		}
	}
	return runs
}

func (f *deploymentVerification) proof(t *testing.T, stage string) {
	t.Helper()
	t.Logf("BE_PROOF {\"stage\":%q,\"elapsed_ms\":%d}", stage, time.Since(f.started).Milliseconds())
}

func deploymentRunFault(t *testing.T, app *testApp, id, body string) func() {
	t.Helper()
	name := pgx.Identifier{"verify_run_" + strings.ReplaceAll(uuid.NewString(), "-", "")}.Sanitize()
	query := fmt.Sprintf(`create function %s() returns trigger language plpgsql as $$ begin if NEW.deployment_external_id = TG_ARGV[0] then %s end if; return NEW; end $$`, name, body)
	if _, err := app.pool.Exec(t.Context(), query); err != nil {
		t.Fatal(err)
	}
	remove := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, statement := range []string{"drop trigger if exists " + name + " on deployment_runs", "drop function if exists " + name + "()"} {
			if _, err := app.pool.Exec(ctx, statement); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(remove)
	query = fmt.Sprintf(`create trigger %s before insert on deployment_runs for each row execute function %s('%s')`, name, name, strings.ReplaceAll(id, "'", "''"))
	if _, err := app.pool.Exec(t.Context(), query); err != nil {
		t.Fatal(err)
	}
	return remove
}

func deploymentRequestRejected(t *testing.T, app *testApp, id string) {
	t.Helper()
	response := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+id+"/run", nil, defaultTestKey, true)
	assertError(t, response, http.StatusBadRequest, "invalid_request_error")
}

func holdDeploymentRun(t *testing.T, app *testApp, id string) func() {
	t.Helper()
	connection, err := pgx.Connect(t.Context(), app.cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	key := time.Now().UnixNano()
	if _, err := connection.Exec(t.Context(), "select pg_advisory_lock($1)", key); err != nil {
		_ = connection.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	remove := deploymentRunFault(t, app, id, fmt.Sprintf("perform pg_advisory_xact_lock(%d);", key))
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		if err := connection.Close(context.Background()); err != nil {
			t.Error(err)
		}
		remove()
	}
	t.Cleanup(release)
	return release
}

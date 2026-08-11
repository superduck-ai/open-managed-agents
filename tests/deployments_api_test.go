package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	deploymentsapi "github.com/superduck-ai/open-managed-agents/internal/deployments"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
	"github.com/superduck-ai/yourbatis"
)

type deploymentAPIResponse struct {
	ID            string          `json:"id"`
	Agent         json.RawMessage `json:"agent"`
	ArchivedAt    *string         `json:"archived_at"`
	CreatedAt     string          `json:"created_at"`
	Description   string          `json:"description"`
	EnvironmentID string          `json:"environment_id"`
	InitialEvents json.RawMessage `json:"initial_events"`
	Metadata      json.RawMessage `json:"metadata"`
	Name          string          `json:"name"`
	PausedReason  json.RawMessage `json:"paused_reason"`
	Resources     json.RawMessage `json:"resources"`
	Schedule      json.RawMessage `json:"schedule"`
	Status        string          `json:"status"`
	Type          string          `json:"type"`
	UpdatedAt     string          `json:"updated_at"`
	VaultIDs      json.RawMessage `json:"vault_ids"`
}

type deploymentPageAPIResponse struct {
	Data     []deploymentAPIResponse `json:"data"`
	NextPage *string                 `json:"next_page"`
}

type deploymentRunAPIResponse struct {
	ID             string          `json:"id"`
	Agent          json.RawMessage `json:"agent"`
	CreatedAt      string          `json:"created_at"`
	DeploymentID   string          `json:"deployment_id"`
	Error          json.RawMessage `json:"error"`
	SessionID      *string         `json:"session_id"`
	TriggerContext json.RawMessage `json:"trigger_context"`
	Type           string          `json:"type"`
}

type deploymentRunPageAPIResponse struct {
	Data     []deploymentRunAPIResponse `json:"data"`
	NextPage *string                    `json:"next_page"`
}

func TestDeploymentsAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("deployments-bucket"))
	defer app.close()

	t.Run("failure API key request missing managed agents beta header", func(t *testing.T) {
		resp := doDeploymentRequest(t, app, http.MethodGet, "/v1/deployments?beta=true", nil, defaultTestKey, false)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure API key request missing Anthropic version header", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, app.baseURL+"/v1/deployments", nil)
		if err != nil {
			t.Fatalf("new deployment request: %v", err)
		}
		req.Header.Set("X-Api-Key", defaultTestKey)
		req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
		resp, err := app.client.Do(req)
		if err != nil {
			t.Fatalf("do deployment request: %v", err)
		}
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("success canonical URLs without beta query", func(t *testing.T) {
		resp := doDeploymentRequest(t, app, http.MethodGet, "/v1/deployments", nil, defaultTestKey, true)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		resp = doDeploymentRequest(t, app, http.MethodGet, "/v1/deployment_runs", nil, defaultTestKey, true)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("deployment runs status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("failure invalid json", func(t *testing.T) {
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(`{"name":`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure missing required fields", func(t *testing.T) {
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(`{"name":"missing"}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("create request contract", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-request-contract-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-request-contract-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		store := createMemoryStore(t, app, "deployments-request-contract-store")

		tests := []struct {
			name string
			body string
		}{
			{
				name: "agent object without type",
				body: `{"agent":{"id":` + quoteJSON(agent.ID) + `},"environment_id":` + quoteJSON(env.ID) + `,"name":"invalid","initial_events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}]}`,
			},
			{name: "null metadata", body: deploymentBodyWithExtra(agent.ID, env.ID, `"metadata":null`)},
			{name: "null resources", body: deploymentBodyWithExtra(agent.ID, env.ID, `"resources":null`)},
			{name: "null vault ids", body: deploymentBodyWithExtra(agent.ID, env.ID, `"vault_ids":null`)},
			{name: "metadata key too long", body: deploymentBodyWithExtra(agent.ID, env.ID, `"metadata":{`+quoteJSON(strings.Repeat("k", 65))+`:"value"}`)},
			{name: "metadata value too long", body: deploymentBodyWithExtra(agent.ID, env.ID, `"metadata":{"key":`+quoteJSON(strings.Repeat("v", 513))+`}`)},
			{name: "message content is not an array", body: deploymentBodyWithInitialEvents(agent.ID, env.ID, `[{"type":"user.message","content":"hello"}]`)},
			{name: "message content is empty", body: deploymentBodyWithInitialEvents(agent.ID, env.ID, `[{"type":"user.message","content":[]}]`)},
			{name: "system message contains image", body: deploymentBodyWithInitialEvents(agent.ID, env.ID, `[{"type":"user.message","content":[{"type":"text","text":"hello"}]},{"type":"system.message","content":[{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}]}]`)},
			{name: "outcome rubric is not an object", body: deploymentBodyWithInitialEvents(agent.ID, env.ID, `[{"type":"user.define_outcome","description":"ship it","rubric":"be correct"}]`)},
			{name: "github token is missing", body: deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"github_repository","url":"https://github.com/example/repo.git"}]`)},
			{name: "memory instructions are not a string", body: deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"memory_store","memory_store_id":`+quoteJSON(store.ID)+`,"instructions":42}]`)},
			{name: "memory instructions are too long", body: deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"memory_store","memory_store_id":`+quoteJSON(store.ID)+`,"instructions":`+quoteJSON(strings.Repeat("i", 4097))+`}]`)},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments", strings.NewReader(test.body), defaultTestKey, true)
				assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
			})
		}

		t.Run("success agent object without version uses latest", func(t *testing.T) {
			created := createDeployment(t, app, `{"agent":{"id":`+quoteJSON(agent.ID)+`,"type":"agent"},"environment_id":`+quoteJSON(env.ID)+`,"name":"latest agent","initial_events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}]}`)
			defer cleanupDeploymentRows(t, app, created.ID)
			assertRawContains(t, created.Agent, `"version":1`)
		})

		t.Run("success agent object with version", func(t *testing.T) {
			created := createDeployment(t, app, `{"agent":{"id":`+quoteJSON(agent.ID)+`,"type":"agent","version":1},"environment_id":`+quoteJSON(env.ID)+`,"name":"versioned agent","initial_events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}]}`)
			defer cleanupDeploymentRows(t, app, created.ID)
			assertRawContains(t, created.Agent, `"version":1`)
		})

		t.Run("success memory instructions may be empty", func(t *testing.T) {
			created := createDeployment(
				t,
				app,
				deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"memory_store","memory_store_id":`+quoteJSON(store.ID)+`,"instructions":""}]`),
			)
			defer cleanupDeploymentRows(t, app, created.ID)
			assertRawContains(t, created.Resources, `"instructions":""`)
		})

	})

	t.Run("failure github resources returned by get require token on update", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-github-round-trip-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-github-round-trip-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		created := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"github_repository","url":"https://github.com/example/repo.git","authorization_token":"secret"}]`))
		defer cleanupDeploymentRows(t, app, created.ID)

		retrieved := retrieveDeployment(t, app, created.ID)
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+created.ID, strings.NewReader(`{"resources":`+string(retrieved.Resources)+`}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure file resource source and mount conflicts", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-file-contract-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-file-contract-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		file := uploadFile(t, app, "deployment-file-contract.txt", "text/plain", []byte("contract"))
		defer deleteFile(t, app, file.ID)

		invalidSource := deploymentBodyWithExtra(
			agent.ID,
			env.ID,
			`"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+`,"source":"/outputs"}]`,
		)
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(invalidSource), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

		duplicateMounts := deploymentBodyWithExtra(
			agent.ID,
			env.ID,
			`"resources":[`+
				`{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/shared.txt"},`+
				`{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/shared.txt"}]`,
		)
		resp = doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(duplicateMounts), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("deployment file resources use the official aggregate resource limit", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-resource-limit-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-resource-limit-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		file := uploadFile(t, app, "deployment-resource-limit.txt", "text/plain", []byte("resource limit"))
		defer deleteFile(t, app, file.ID)

		t.Run("failure 501 file resources", func(t *testing.T) {
			body := deploymentBodyWithFileResources(agent.ID, env.ID, file.ID, 501)
			resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments", strings.NewReader(body), defaultTestKey, true)
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		})

		t.Run("success 500 file resources and run", func(t *testing.T) {
			body := deploymentBodyWithFileResources(agent.ID, env.ID, file.ID, 500)
			created := createDeployment(t, app, body)
			defer cleanupDeploymentRows(t, app, created.ID)
			var resources []json.RawMessage
			if err := json.Unmarshal(created.Resources, &resources); err != nil {
				t.Fatalf("decode resources: %v", err)
			}
			if len(resources) != 500 {
				t.Fatalf("resources = %d, want 500", len(resources))
			}
			run := runDeployment(t, app, created.ID)
			if run.SessionID == nil || *run.SessionID == "" {
				t.Fatalf("deployment run Session ID = nil: %+v", run)
			}
			defer deleteSession(t, app, *run.SessionID)
		})

		t.Run("update uses the same file limit", func(t *testing.T) {
			created := createDeployment(t, app, minimalDeploymentBody(agent.ID, env.ID))
			defer cleanupDeploymentRows(t, app, created.ID)

			resp := doDeploymentRequest(
				t,
				app,
				http.MethodPost,
				"/v1/deployments/"+created.ID,
				strings.NewReader(`{"resources":`+fileResourcesJSON(file.ID, 501)+`}`),
				defaultTestKey,
				true,
			)
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

			updated := updateDeployment(t, app, created.ID, `{"resources":`+fileResourcesJSON(file.ID, 500)+`}`)
			var resources []json.RawMessage
			if err := json.Unmarshal(updated.Resources, &resources); err != nil {
				t.Fatalf("decode resources: %v", err)
			}
			if len(resources) != 500 {
				t.Fatalf("resources = %d, want 500", len(resources))
			}
		})
	})

	t.Run("update preserves omitted fields and clears nullable replacements", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-update-contract-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-update-contract-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		file := uploadFile(t, app, "deployment-update-contract.txt", "text/plain", []byte("contract"))
		defer deleteFile(t, app, file.ID)
		vault := createVault(t, app, `{"display_name":"deployments update contract"}`)
		defer deleteVault(t, app, vault.ID)

		created := createDeployment(t, app, `{
			"agent":`+quoteJSON(agent.ID)+`,
			"environment_id":`+quoteJSON(env.ID)+`,
			"name":"update contract",
			"description":"preserve me",
			"initial_events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}],
			"metadata":{"keep":"yes"},
			"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+`}],
			"vault_ids":[`+quoteJSON(vault.ID)+`]
		}`)
		defer cleanupDeploymentRows(t, app, created.ID)

		resp := doDeploymentRequest(
			t,
			app,
			http.MethodPost,
			"/v1/deployments/"+created.ID,
			strings.NewReader(`{"metadata":null}`),
			defaultTestKey,
			true,
		)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

		roundTripped := updateDeployment(t, app, created.ID, `{"resources":`+string(created.Resources)+`}`)
		assertRawContains(t, roundTripped.Resources, `"file_id":"`+file.ID+`"`)
		assertRawContains(t, roundTripped.Resources, `"mount_path":"/uploads/`+file.Filename+`"`)
		run := runDeployment(t, app, created.ID)
		if run.SessionID == nil {
			t.Fatalf("deployment run Session ID = nil: %+v", run)
		}
		defer deleteSession(t, app, *run.SessionID)
		resources, err := app.db.ListSessionResources(context.Background(), getDefaultDBIDs(t, app.pool).WorkspaceUUID, *run.SessionID)
		if err != nil || len(resources) != 1 {
			t.Fatalf("deployment run Session resources = %d, error = %v", len(resources), err)
		}
		assertSessionFileReference(t, app, *run.SessionID, resources[0].Payload, file.ID, "/uploads/"+file.Filename)

		preserved := updateDeployment(t, app, created.ID, `{"name":"updated name only"}`)
		if preserved.Description != "preserve me" {
			t.Fatalf("description = %v, want preserved value", preserved.Description)
		}
		assertRawContains(t, preserved.Metadata, `"keep":"yes"`)
		assertRawContains(t, preserved.Resources, `"file_id":"`+file.ID+`"`)
		assertRawContains(t, preserved.VaultIDs, quoteJSON(vault.ID))

		cleared := updateDeployment(t, app, created.ID, `{"description":"","resources":null,"vault_ids":null}`)
		if cleared.Description != "" || string(cleared.Resources) != "[]" || string(cleared.VaultIDs) != "[]" {
			t.Fatalf("nullable replacements were not cleared: %+v", cleared)
		}
		assertRawContains(t, cleared.Metadata, `"keep":"yes"`)

		withDescription := updateDeployment(t, app, created.ID, `{"description":"clear with null"}`)
		if withDescription.Description == "" {
			t.Fatal("description was not restored before null clear")
		}
		cleared = updateDeployment(t, app, created.ID, `{"description":null}`)
		if cleared.Description != "" {
			t.Fatalf("description = %q, want empty string", cleared.Description)
		}
	})

	t.Run("failure invalid schedule", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-bad-schedule-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-bad-schedule-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		body := deploymentBodyWithExtra(agent.ID, env.ID, `"schedule":{"type":"cron","expression":"bad","timezone":"UTC"}`)
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("startup skips a bad stored schedule", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-startup-schedule-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-startup-schedule-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		good := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, env.ID, `"schedule":{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}`))
		defer cleanupDeploymentRows(t, app, good.ID)
		bad := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, env.ID, `"schedule":{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}`))
		defer cleanupDeploymentRows(t, app, bad.ID)

		if _, err := app.pool.Exec(context.Background(), `
			update deployments
			set schedule = '{"type":"cron","expression":"bad","timezone":"UTC"}'::jsonb
			where external_id = $1
		`, bad.ID); err != nil {
			t.Fatalf("prepare invalid stored schedule: %v", err)
		}
		stopScheduler := startDeploymentScheduler(t, app, nil)
		stopScheduler()

	})

	t.Run("execution update invalidates a stale occurrence", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-execution-revision-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-execution-revision-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		created := createDeployment(t, app, `{
			"agent":`+quoteJSON(agent.ID)+`,
			"environment_id":`+quoteJSON(env.ID)+`,
			"name":"execution revision",
			"initial_events":[{"type":"user.message","content":[{"type":"text","text":"before"}]}],
			"schedule":{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}
		}`)
		defer cleanupDeploymentRows(t, app, created.ID)

		ctx := context.Background()
		ids := getDefaultDBIDs(t, app.pool)
		stale, err := app.db.GetDeployment(ctx, ids.WorkspaceUUID, created.ID)
		if err != nil {
			t.Fatalf("load scheduled deployment: %v", err)
		}
		updateDeployment(t, app, created.ID, `{"initial_events":[{"type":"user.message","content":[{"type":"text","text":"after"}]}]}`)
		current, err := app.db.GetDeployment(ctx, ids.WorkspaceUUID, created.ID)
		if err != nil {
			t.Fatalf("load updated deployment: %v", err)
		}
		if current.ScheduleRevision != stale.ScheduleRevision+1 {
			t.Fatalf("schedule_revision = %d, want %d", current.ScheduleRevision, stale.ScheduleRevision+1)
		}
		err = applyScheduledOccurrence(ctx, app.db, db.ApplyScheduledOccurrenceInput{
			WorkspaceUUID: ids.WorkspaceUUID, DeploymentExternalID: created.ID,
			ScheduleRevision: stale.ScheduleRevision, ScheduledAt: time.Now().UTC().Truncate(time.Minute),
		})
		if !errors.Is(err, db.ErrStaleSchedule) {
			t.Fatalf("ApplyScheduledOccurrence() error = %v, want ErrStaleSchedule", err)
		}
	})

	t.Run("duplicate scheduled occurrence creates one run", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-occurrence-idempotency-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-occurrence-idempotency-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		created := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, env.ID, `"schedule":{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}`))
		defer cleanupDeploymentRows(t, app, created.ID)

		ctx := context.Background()
		ids := getDefaultDBIDs(t, app.pool)
		deployment, err := app.db.GetDeployment(ctx, ids.WorkspaceUUID, created.ID)
		if err != nil {
			t.Fatalf("load scheduled deployment: %v", err)
		}
		scheduledAt := time.Now().UTC().Truncate(time.Minute)
		input := db.ApplyScheduledOccurrenceInput{
			WorkspaceUUID: ids.WorkspaceUUID, DeploymentExternalID: created.ID,
			ScheduleRevision: deployment.ScheduleRevision, ScheduledAt: scheduledAt,
			Run: db.DeploymentRun{
				UUID: uuid.NewString(), ExternalID: "drun_periodic_" + uuid.NewString(),
			},
			Now: scheduledAt,
		}
		if err := applyScheduledOccurrence(ctx, app.db, input); err != nil {
			t.Fatalf("apply first scheduled occurrence: %v", err)
		}
		input.Run.UUID = uuid.NewString()
		input.Run.ExternalID = "drun_periodic_" + uuid.NewString()
		if err := applyScheduledOccurrence(ctx, app.db, input); !errors.Is(err, db.ErrStaleSchedule) {
			t.Fatalf("apply duplicate scheduled occurrence error = %v, want ErrStaleSchedule", err)
		}
		runs, _, err := app.db.ListDeploymentRunsPage(ctx, db.ListDeploymentRunsPageParams{
			WorkspaceUUID: ids.WorkspaceUUID, DeploymentExternalID: created.ID, Limit: 10,
		})
		if err != nil || len(runs) != 1 {
			t.Fatalf("scheduled runs = (%d, %v), want one", len(runs), err)
		}
		if runs[0].TriggerType != "schedule" || runs[0].ScheduledAt == nil || !runs[0].ScheduledAt.Equal(scheduledAt) ||
			runs[0].CreatedByAPIKeyUUID != deployment.CreatedByAPIKeyUUID {
			t.Fatalf("scheduled run fields = %+v", runs[0])
		}
	})

	t.Run("failure archived workspace rejects a scheduled session", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-archived-workspace-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-archived-workspace-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		created := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, env.ID, `"schedule":{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}`))
		defer cleanupDeploymentRows(t, app, created.ID)

		ctx := context.Background()
		ids := getDefaultDBIDs(t, app.pool)
		deployment, err := app.db.GetDeployment(ctx, ids.WorkspaceUUID, created.ID)
		if err != nil {
			t.Fatalf("load scheduled deployment = (%+v, %v)", deployment, err)
		}
		scheduledAt := time.Now().UTC().Truncate(time.Minute)
		if _, err := app.db.ArchiveAdminWorkspace(ctx, ids.OrganizationUUID, "workspace_default"); err != nil {
			t.Fatalf("archive workspace: %v", err)
		}
		defer func() {
			if _, err := app.pool.Exec(context.Background(), `update workspaces set archived_at = null where uuid = $1`, ids.WorkspaceUUID); err != nil {
				t.Errorf("restore workspace: %v", err)
			}
		}()

		err = applyScheduledOccurrence(ctx, app.db, db.ApplyScheduledOccurrenceInput{
			WorkspaceUUID: ids.WorkspaceUUID, DeploymentExternalID: created.ID,
			ScheduleRevision: deployment.ScheduleRevision, ScheduledAt: scheduledAt,
			Session: &db.CreateSessionInput{},
		})
		if !errors.Is(err, db.ErrWorkspaceArchived) {
			t.Fatalf("ApplyScheduledOccurrence() error = %v, want ErrWorkspaceArchived", err)
		}
		after, loadErr := app.db.GetDeployment(ctx, ids.WorkspaceUUID, created.ID)
		if loadErr != nil || after.Status != "active" || after.ScheduleRevision != deployment.ScheduleRevision || after.LastRunAt != nil {
			t.Fatalf("deployment after rejected occurrence = (%+v, %v), want unchanged", after, loadErr)
		}
	})

	t.Run("failure auto pause rolls back when outbox cannot be written", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-outbox-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-outbox-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		created := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, env.ID, `"schedule":{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}`))
		defer cleanupDeploymentRows(t, app, created.ID)

		ids := getDefaultDBIDs(t, app.pool)
		now := time.Now().UTC()
		endpoint, err := app.db.CreateWebhookEndpoint(context.Background(), db.WebhookEndpoint{
			UUID: uuid.NewString(), ExternalID: "wh_outbox_" + uuid.NewString(),
			OrganizationUUID: ids.OrganizationUUID, WorkspaceUUID: ids.WorkspaceUUID,
			CreatedByAPIKeyUUID: ids.APIKeyUUID, URL: "https://example.test/webhook",
			Name: "outbox rollback", EnabledEvents: []string{
				"deployment_run.started", "deployment_run.failed", "deployment.paused",
			},
			SigningSecret: "secret", Status: "enabled", CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatalf("create webhook endpoint: %v", err)
		}
		defer func() {
			if err := app.db.DeleteWebhookEndpoint(context.Background(), ids.WorkspaceUUID, endpoint.ExternalID); err != nil {
				t.Errorf("delete webhook endpoint: %v", err)
			}
		}()

		deployment, err := app.db.GetDeployment(context.Background(), ids.WorkspaceUUID, created.ID)
		if err != nil {
			t.Fatalf("load scheduled deployment = (%+v, %v)", deployment, err)
		}
		scheduledAt := time.Now().UTC().Truncate(time.Minute)
		runID := "drun_outbox_" + uuid.NewString()
		err = applyScheduledOccurrence(context.Background(), app.db, db.ApplyScheduledOccurrenceInput{
			WorkspaceUUID: ids.WorkspaceUUID, DeploymentExternalID: deployment.ExternalID,
			ScheduleRevision: deployment.ScheduleRevision, ScheduledAt: scheduledAt,
			Run: db.DeploymentRun{
				UUID: uuid.NewString(), ExternalID: runID,
			},
			AutoPauseReason: json.RawMessage(`{"type":"error","error":{"type":"agent_archived_error"}}`),
			WebhookEvents: []db.WebhookDeliveryEvent{
				{EventType: "deployment_run.started", Event: json.RawMessage(`{}`), FallbackEnabled: true},
				{EventType: "deployment_run.failed", Event: json.RawMessage(`{}`), FallbackEnabled: true},
				{EventType: "deployment.paused", Event: json.RawMessage(`{`), FallbackEnabled: true},
			},
			Now: now,
		})
		if err == nil {
			t.Fatal("ApplyScheduledOccurrence() error = nil")
		}
		after, loadErr := app.db.GetDeployment(context.Background(), ids.WorkspaceUUID, created.ID)
		if loadErr != nil || after.Status != "active" || after.ScheduleRevision != deployment.ScheduleRevision || after.LastRunAt != nil {
			t.Fatalf("deployment after failed outbox = (%+v, %v), want unchanged", after, loadErr)
		}
		if _, loadErr := app.db.GetDeploymentRun(context.Background(), ids.WorkspaceUUID, runID); !errors.Is(loadErr, db.ErrNotFound) {
			t.Fatalf("GetDeploymentRun() error = %v, want ErrNotFound", loadErr)
		}
	})

	t.Run("failure scheduled root agent archive rolls back when outbox cannot be written", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-scheduled-archive-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-scheduled-archive-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		created := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, env.ID, `"schedule":{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}`))
		defer cleanupDeploymentRows(t, app, created.ID)

		ctx := context.Background()
		ids := getDefaultDBIDs(t, app.pool)
		deployment, err := app.db.GetDeployment(ctx, ids.WorkspaceUUID, created.ID)
		if err != nil {
			t.Fatalf("load scheduled deployment = (%+v, %v)", deployment, err)
		}
		err = applyScheduledOccurrence(ctx, app.db, db.ApplyScheduledOccurrenceInput{
			WorkspaceUUID: ids.WorkspaceUUID, DeploymentExternalID: created.ID,
			ScheduleRevision: deployment.ScheduleRevision, ScheduledAt: time.Now().UTC().Truncate(time.Minute),
			ArchiveDeployment: true,
			WebhookEvents: []db.WebhookDeliveryEvent{{
				EventType: "deployment.archived", Event: json.RawMessage(`{`), FallbackEnabled: true,
			}},
		})
		if err == nil {
			t.Fatal("ApplyScheduledOccurrence() error = nil")
		}
		after, loadErr := app.db.GetDeployment(ctx, ids.WorkspaceUUID, created.ID)
		if loadErr != nil || after.ArchivedAt != nil {
			t.Fatalf("deployment after failed archive outbox = (%+v, %v), want active", after, loadErr)
		}
	})

	t.Run("failure agent archive rolls back deployments when outbox cannot be written", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-agent-archive-outbox-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-agent-archive-outbox-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		created := createDeployment(t, app, minimalDeploymentBody(agent.ID, env.ID))
		defer cleanupDeploymentRows(t, app, created.ID)

		ctx := context.Background()
		ids := getDefaultDBIDs(t, app.pool)
		_, _, err := app.db.ArchiveAgent(ctx, ids.WorkspaceUUID, agent.ID, func(db.Deployment) (db.WebhookDeliveryEvent, error) {
			return db.WebhookDeliveryEvent{
				EventType: "deployment.archived", Event: json.RawMessage(`{`), FallbackEnabled: true,
			}, nil
		})
		if err == nil {
			t.Fatal("ArchiveAgent() error = nil")
		}
		afterAgent, agentErr := app.db.GetAgent(ctx, ids.WorkspaceUUID, agent.ID)
		if agentErr != nil || afterAgent.ArchivedAt != nil {
			t.Fatalf("agent after failed outbox = (%+v, %v), want active", afterAgent, agentErr)
		}
		afterDeployment, deploymentErr := app.db.GetDeployment(ctx, ids.WorkspaceUUID, created.ID)
		if deploymentErr != nil || afterDeployment.ArchivedAt != nil {
			t.Fatalf("deployment after failed outbox = (%+v, %v), want active", afterDeployment, deploymentErr)
		}
	})

	t.Run("agent API archives deployments with webhook outbox", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-agent-archive-webhook-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-agent-archive-webhook-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		created := createDeployment(t, app, minimalDeploymentBody(agent.ID, env.ID))
		defer cleanupDeploymentRows(t, app, created.ID)

		ctx := context.Background()
		ids := getDefaultDBIDs(t, app.pool)
		now := time.Now().UTC()
		endpoint, err := app.db.CreateWebhookEndpoint(ctx, db.WebhookEndpoint{
			UUID: uuid.NewString(), ExternalID: "wh_agent_archive_" + uuid.NewString(),
			OrganizationUUID: ids.OrganizationUUID, WorkspaceUUID: ids.WorkspaceUUID,
			CreatedByAPIKeyUUID: ids.APIKeyUUID, URL: "https://example.test/webhook",
			Name: "agent archive", EnabledEvents: []string{"deployment.archived"},
			SigningSecret: "secret", Status: "enabled", CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatalf("create webhook endpoint: %v", err)
		}
		defer func() {
			if _, err := app.pool.Exec(context.Background(), `delete from jobs where payload->>'webhook_endpoint_uuid' = $1`, endpoint.UUID); err != nil {
				t.Errorf("cleanup webhook jobs: %v", err)
			}
			if err := app.db.DeleteWebhookEndpoint(context.Background(), ids.WorkspaceUUID, endpoint.ExternalID); err != nil {
				t.Errorf("delete webhook endpoint: %v", err)
			}
		}()

		archiveAgent(t, app, agent.ID)
		archived, err := app.db.GetDeployment(ctx, ids.WorkspaceUUID, created.ID)
		if err != nil || archived.ArchivedAt == nil {
			t.Fatalf("deployment after agent API archive = (%+v, %v), want archived", archived, err)
		}
		var jobs int
		if err := app.pool.QueryRow(ctx, `
			select count(*)
			from jobs
			where payload->>'webhook_endpoint_uuid' = $1
			and payload->>'event_type' = 'deployment.archived'
			and payload->'event'->'data'->>'id' = $2
		`, endpoint.UUID, created.ID).Scan(&jobs); err != nil {
			t.Fatalf("count deployment archive webhooks: %v", err)
		}
		if jobs != 1 {
			t.Fatalf("deployment archive webhooks = %d, want 1", jobs)
		}
	})

	t.Run("failure archived agent is rejected", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-archived-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-archived-agent-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		archiveAgent(t, app, agent.ID)

		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(minimalDeploymentBody(agent.ID, env.ID)), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure archived environment is rejected", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-archived-env-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-archived-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		archiveEnvironment(t, app, env.ID)

		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(minimalDeploymentBody(agent.ID, env.ID)), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure archived vault is rejected", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-archived-vault-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-archived-vault-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		vault := createVault(t, app, `{"display_name":"deployments archived vault"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		archiveVault(t, app, vault.ID)

		body := deploymentBodyWithExtra(agent.ID, env.ID, `"vault_ids":[`+quoteJSON(vault.ID)+`]`)
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure deleted file resource is rejected", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-deleted-file-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-deleted-file-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		file := uploadFile(t, app, "deployment-deleted-resource.txt", "text/plain", []byte("deleted resource"))
		deleteFile(t, app, file.ID)

		body := deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+`}]`)
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("failure list status conflicts with include archived", func(t *testing.T) {
		for _, includeArchived := range []string{"true", "false"} {
			resp := doDeploymentRequest(t, app, http.MethodGet, "/v1/deployments?beta=true&status=active&include_archived="+includeArchived, nil, defaultTestKey, true)
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		}
	})

	t.Run("success manual run records reference error", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-run-error-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-run-error-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		file := uploadFile(t, app, "deployment-run-error-resource.txt", "text/plain", []byte("run error resource"))

		created := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+`}]`))
		defer cleanupDeploymentRows(t, app, created.ID)
		deleteFile(t, app, file.ID)

		run := runDeployment(t, app, created.ID)
		if !strings.HasPrefix(run.ID, "drun_") || run.DeploymentID != created.ID || run.SessionID != nil {
			t.Fatalf("unexpected failed deployment run shell: %+v", run)
		}
		if !strings.Contains(string(run.TriggerContext), `"manual"`) || !strings.Contains(string(run.Error), `"file_not_found_error"`) {
			t.Fatalf("unexpected failed deployment run error: %+v", run)
		}

		runs := listDeploymentRuns(t, app, "deployment_id="+url.QueryEscape(created.ID)+"&trigger_type=manual&has_error=true")
		if !containsDeploymentRun(runs.Data, run.ID) {
			t.Fatalf("failed run list missing %s: %+v", run.ID, runs.Data)
		}
	})

	t.Run("success manual run binds file resource into session filesystem", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-file-run-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-file-run-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		file := uploadFile(t, app, "deployment-file-run.txt", "text/plain", []byte("deployment file run"))
		defer deleteFile(t, app, file.ID)

		created := createDeployment(
			t,
			app,
			deploymentBodyWithExtra(
				agent.ID,
				env.ID,
				`"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/workspace/deployment.txt"}]`,
			),
		)
		defer cleanupDeploymentRows(t, app, created.ID)
		if created.Description != "" {
			t.Fatalf("description = %q, want empty string", created.Description)
		}
		assertRawNotContains(t, created.Resources, `"source"`)
		assertRawContains(t, created.Resources, `"mount_path":"/uploads/workspace/deployment.txt"`)
		run := runDeployment(t, app, created.ID)
		if run.SessionID == nil || *run.SessionID == "" {
			t.Fatalf("deployment run Session ID = nil: %+v", run)
		}
		defer deleteSession(t, app, *run.SessionID)

		runSession := retrieveSession(t, app, *run.SessionID, defaultTestKey)
		if len(runSession.Resources) != 1 {
			t.Fatalf("deployment run Session resources = %d, want 1", len(runSession.Resources))
		}
		assertSessionFileReference(
			t,
			app,
			*run.SessionID,
			runSession.Resources[0],
			file.ID,
			"/uploads/workspace/deployment.txt",
		)
	})

	t.Run("success initial user messages replay in order", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployment-initial-history-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployment-initial-history-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		deployment := createDeployment(t, app, `{
			"agent":`+quoteJSON(agent.ID)+`,
			"environment_id":`+quoteJSON(env.ID)+`,
			"name":"deployment initial history",
			"initial_events":[
				{"type":"user.message","content":[{"type":"text","text":"deployment first"}]},
				{"type":"user.message","content":[{"type":"text","text":"deployment second"}]},
				{"type":"system.message","content":[{"type":"text","text":"public only"}]}
			]
		}`)
		defer cleanupDeploymentRows(t, app, deployment.ID)
		run := runDeployment(t, app, deployment.ID)
		if run.SessionID == nil || *run.SessionID == "" {
			t.Fatalf("deployment initial history Session ID = nil: %+v", run)
		}
		defer deleteSession(t, app, *run.SessionID)

		codeSessionID := launchLocalCodeSession(t, app, *run.SessionID)
		inbound, err := app.db.ListQueuedCodeSessionInboundEvents(context.Background(), codeSessionID)
		if err != nil {
			t.Fatalf("list deployment startup inbound: %v", err)
		}
		if len(inbound) != 3 ||
			inbound[0].EventSubtype != "initialize" ||
			!strings.Contains(string(inbound[1].Payload), "deployment first") ||
			!strings.Contains(string(inbound[2].Payload), "deployment second") {
			t.Fatalf("deployment startup inbound = %#v, want initialize, first, second", inbound)
		}
	})

	t.Run("success lifecycle manual run session events and run filters", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-api-agent"}`)
		defer cleanupAgentRows(t, app.pool, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-api-env"}`)
		defer cleanupEnvironmentRows(t, app.pool, env.ID)
		file := uploadFile(t, app, "deployment-resource.txt", "text/plain", []byte("deployment file"))
		defer deleteFile(t, app, file.ID)

		created := createDeployment(t, app, `{
			"agent":`+quoteJSON(agent.ID)+`,
			"environment_id":`+quoteJSON(env.ID)+`,
			"name":"Nightly order triage",
			"description":"handles orders",
			"metadata":{"case":"1234"},
			"initial_events":[{"type":"user.message","content":[{"type":"text","text":"Where is my order?"}]}],
			"resources":[
				{"type":"file","file_id":`+quoteJSON(file.ID)+`},
				{"type":"github_repository","url":"https://github.com/example/repo.git","authorization_token":"secret-token"}
			],
			"schedule":{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}
		}`)
		if created.Type != "deployment" || !strings.HasPrefix(created.ID, "depl_") || created.Status != "active" {
			t.Fatalf("unexpected created deployment: %+v", created)
		}
		if created.EnvironmentID != env.ID || created.Description != "handles orders" {
			t.Fatalf("unexpected deployment env/description: %+v", created)
		}
		assertRawContains(t, created.Metadata, `"case":"1234"`)
		assertRawContains(t, created.Schedule, `"upcoming_runs_at"`)
		assertRawContains(t, created.Resources, `"github_repository"`)
		assertRawNotContains(t, created.Resources, `"source"`)
		assertRawContains(t, created.Resources, `"mount_path":"/uploads/`+file.Filename+`"`)
		assertRawNotContains(t, created.Resources, "secret-token")

		listed := listDeployments(t, app, "agent_id="+url.QueryEscape(agent.ID))
		if !containsDeployment(listed.Data, created.ID) {
			t.Fatalf("deployment list missing %s: %+v", created.ID, listed.Data)
		}
		retrieved := retrieveDeployment(t, app, created.ID)
		if retrieved.ID != created.ID {
			t.Fatalf("retrieved deployment id = %s, want %s", retrieved.ID, created.ID)
		}

		updated := updateDeployment(t, app, created.ID, `{
			"name":"Updated order triage",
			"metadata":{"case":"","priority":"high"},
			"resources":[],
			"schedule":null
		}`)
		if updated.Name != "Updated order triage" {
			t.Fatalf("updated name = %s", updated.Name)
		}
		assertRawContains(t, updated.Metadata, `"case":""`)
		assertRawContains(t, updated.Metadata, `"priority":"high"`)
		if string(updated.Resources) != "[]" || string(updated.Schedule) != "null" {
			t.Fatalf("unexpected updated resources/schedule: resources=%s schedule=%s", updated.Resources, updated.Schedule)
		}

		updated = updateDeployment(t, app, created.ID, `{"metadata":{"case":null}}`)
		assertRawNotContains(t, updated.Metadata, `"case"`)
		assertRawContains(t, updated.Metadata, `"priority":"high"`)

		paused := pauseDeployment(t, app, created.ID)
		if paused.Status != "paused" || !strings.Contains(string(paused.PausedReason), `"manual"`) {
			t.Fatalf("unexpected paused deployment: %+v", paused)
		}
		pausedRun := runDeployment(t, app, created.ID)
		if pausedRun.SessionID == nil || *pausedRun.SessionID == "" {
			t.Fatalf("paused deployment run = %+v", pausedRun)
		}
		defer deleteSession(t, app, *pausedRun.SessionID)
		stillPaused := retrieveDeployment(t, app, created.ID)
		if stillPaused.Status != "paused" {
			t.Fatalf("deployment status after manual run = %s, want paused", stillPaused.Status)
		}

		unpaused := unpauseDeployment(t, app, created.ID)
		if unpaused.Status != "active" || string(unpaused.PausedReason) != "null" {
			t.Fatalf("unexpected unpaused deployment: %+v", unpaused)
		}

		run := runDeployment(t, app, created.ID)
		if !strings.HasPrefix(run.ID, "drun_") || run.DeploymentID != created.ID || run.SessionID == nil || *run.SessionID == "" {
			t.Fatalf("unexpected deployment run: %+v", run)
		}
		var filesystemCount int
		if err := app.pool.QueryRow(context.Background(), `
			select count(*)
			from filestore_filesystems fs
			join workspaces w on w.uuid = fs.workspace_uuid
				join sessions s on s.uuid = fs.session_uuid and s.workspace_uuid = w.uuid
			where s.external_id = $1 and fs.deleted_at is null
		`, *run.SessionID).Scan(&filesystemCount); err != nil {
			t.Fatalf("count deployment Session filesystem: %v", err)
		}
		if filesystemCount != 1 {
			t.Fatalf("deployment Session filesystems = %d, want 1", filesystemCount)
		}
		if !strings.Contains(string(run.TriggerContext), `"manual"`) || string(run.Error) != "null" {
			t.Fatalf("unexpected run trigger/error: %+v", run)
		}
		gotRun := retrieveDeploymentRun(t, app, run.ID)
		if gotRun.ID != run.ID || gotRun.SessionID == nil || *gotRun.SessionID != *run.SessionID {
			t.Fatalf("retrieve run = %+v, want session %s", gotRun, *run.SessionID)
		}
		runs := listDeploymentRuns(t, app, "deployment_id="+url.QueryEscape(created.ID)+"&trigger_type=manual&has_error=false")
		if !containsDeploymentRun(runs.Data, run.ID) {
			t.Fatalf("run list missing %s: %+v", run.ID, runs.Data)
		}
		missingDeploymentRuns := listDeploymentRuns(t, app, "deployment_id="+url.QueryEscape("depl_missing_test"))
		if len(missingDeploymentRuns.Data) != 0 {
			t.Fatalf("missing deployment run list = %+v, want empty data", missingDeploymentRuns.Data)
		}

		platformUnauthResp := app.platformRequest(t, http.MethodGet, "/v1/deployment_runs?beta=true", nil, nil)
		assertError(t, platformUnauthResp, http.StatusUnauthorized, "authentication_error")

		platformCookies := app.platformLoginCookies(t, "deployments-platform-runs@example.com")
		platformListResp := app.platformRequest(t, http.MethodGet, "/v1/deployment_runs?beta=true&limit=5&deployment_id="+url.QueryEscape(created.ID), nil, platformCookies)
		defer platformListResp.Body.Close()
		if platformListResp.StatusCode != http.StatusOK {
			t.Fatalf("platform list deployment runs status = %d, want 200: %s", platformListResp.StatusCode, readAll(t, platformListResp.Body))
		}
		var platformRuns deploymentRunPageAPIResponse
		decodeJSON(t, platformListResp.Body, &platformRuns)
		if !containsDeploymentRun(platformRuns.Data, run.ID) {
			t.Fatalf("platform run list missing %s: %+v", run.ID, platformRuns.Data)
		}

		platformGetResp := app.platformRequest(t, http.MethodGet, "/v1/deployment_runs/"+run.ID+"?beta=true", nil, platformCookies)
		defer platformGetResp.Body.Close()
		if platformGetResp.StatusCode != http.StatusOK {
			t.Fatalf("platform get deployment run status = %d, want 200: %s", platformGetResp.StatusCode, readAll(t, platformGetResp.Body))
		}
		var platformRun deploymentRunAPIResponse
		decodeJSON(t, platformGetResp.Body, &platformRun)
		if platformRun.ID != run.ID || platformRun.DeploymentID != created.ID {
			t.Fatalf("platform get deployment run = %+v, want run %s deployment %s", platformRun, run.ID, created.ID)
		}

		session := retrieveSession(t, app, *run.SessionID, defaultTestKey)
		if session.DeploymentID == nil || *session.DeploymentID != created.ID || session.EnvironmentID != env.ID {
			t.Fatalf("unexpected run-created session: %+v", session)
		}
		workType, workSessionID, workState := sessionWorkData(t, app, session.ID)
		if workType != "session" || workSessionID != session.ID || workState != "queued" {
			t.Fatalf("unexpected run-created work type=%s session_id=%s state=%s", workType, workSessionID, workState)
		}
		sessions := listSessions(t, app, "deployment_id="+url.QueryEscape(created.ID))
		if !containsSession(sessions.Data, session.ID) {
			t.Fatalf("session list missing run-created session: %+v", sessions.Data)
		}
		events := listSessionEvents(t, app, session.ID, "", defaultTestKey)
		if len(events.Data) != 1 || !strings.Contains(string(events.Data[0]), `"type":"user.message"`) {
			t.Fatalf("unexpected initial events: %+v", events)
		}

		archived := archiveDeployment(t, app, created.ID)
		if archived.ArchivedAt == nil {
			t.Fatalf("archived_at = nil")
		}
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+created.ID+"/run?beta=true", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})
}

func doDeploymentRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, beta bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new deployment request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	if beta {
		req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do deployment request: %v", err)
	}
	return resp
}

func createDeployment(t *testing.T, app *testApp, body string) deploymentAPIResponse {
	t.Helper()
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create deployment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var deployment deploymentAPIResponse
	decodeJSON(t, resp.Body, &deployment)
	if deployment.ID == "" {
		t.Fatalf("create deployment returned empty id: %+v", deployment)
	}
	return deployment
}

func retrieveDeployment(t *testing.T, app *testApp, deploymentID string) deploymentAPIResponse {
	t.Helper()
	resp := doDeploymentRequest(t, app, http.MethodGet, "/v1/deployments/"+deploymentID, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve deployment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var deployment deploymentAPIResponse
	decodeJSON(t, resp.Body, &deployment)
	return deployment
}

func updateDeployment(t *testing.T, app *testApp, deploymentID, body string) deploymentAPIResponse {
	t.Helper()
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deploymentID, strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update deployment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var deployment deploymentAPIResponse
	decodeJSON(t, resp.Body, &deployment)
	return deployment
}

func pauseDeployment(t *testing.T, app *testApp, deploymentID string) deploymentAPIResponse {
	t.Helper()
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deploymentID+"/pause", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pause deployment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var deployment deploymentAPIResponse
	decodeJSON(t, resp.Body, &deployment)
	return deployment
}

func unpauseDeployment(t *testing.T, app *testApp, deploymentID string) deploymentAPIResponse {
	t.Helper()
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deploymentID+"/unpause", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unpause deployment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var deployment deploymentAPIResponse
	decodeJSON(t, resp.Body, &deployment)
	return deployment
}

func archiveDeployment(t *testing.T, app *testApp, deploymentID string) deploymentAPIResponse {
	t.Helper()
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deploymentID+"/archive", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive deployment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var deployment deploymentAPIResponse
	decodeJSON(t, resp.Body, &deployment)
	return deployment
}

func runDeployment(t *testing.T, app *testApp, deploymentID string) deploymentRunAPIResponse {
	t.Helper()
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deploymentID+"/run", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("run deployment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var run deploymentRunAPIResponse
	decodeJSON(t, resp.Body, &run)
	return run
}

func retrieveDeploymentRun(t *testing.T, app *testApp, runID string) deploymentRunAPIResponse {
	t.Helper()
	resp := doDeploymentRequest(t, app, http.MethodGet, "/v1/deployment_runs/"+runID, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve deployment run status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var run deploymentRunAPIResponse
	decodeJSON(t, resp.Body, &run)
	return run
}

func listDeployments(t *testing.T, app *testApp, query string) deploymentPageAPIResponse {
	t.Helper()
	path := "/v1/deployments"
	if query != "" {
		path += "?" + query
	}
	resp := doDeploymentRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list deployments status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page deploymentPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func listDeploymentRuns(t *testing.T, app *testApp, query string) deploymentRunPageAPIResponse {
	t.Helper()
	path := "/v1/deployment_runs"
	if query != "" {
		path += "?" + query
	}
	resp := doDeploymentRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list deployment runs status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page deploymentRunPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func minimalDeploymentBody(agentID, envID string) string {
	return deploymentBodyWithExtra(agentID, envID, "")
}

func deploymentBodyWithExtra(agentID, envID, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return `{
		"agent":` + quoteJSON(agentID) + `,
		"environment_id":` + quoteJSON(envID) + `,
		"name":"minimal deployment",
		"initial_events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}]
		` + extra + `
	}`
}

func deploymentBodyWithInitialEvents(agentID, envID, events string) string {
	return `{
		"agent":` + quoteJSON(agentID) + `,
		"environment_id":` + quoteJSON(envID) + `,
		"name":"initial events deployment",
		"initial_events":` + events + `
	}`
}

func deploymentBodyWithFileResources(agentID, envID, fileID string, count int) string {
	return deploymentBodyWithExtra(agentID, envID, `"resources":`+fileResourcesJSON(fileID, count))
}

func fileResourcesJSON(fileID string, count int) string {
	resources := make([]map[string]string, count)
	for index := range resources {
		resources[index] = map[string]string{
			"type":       "file",
			"file_id":    fileID,
			"mount_path": "/resource-" + strconv.Itoa(index),
		}
	}
	raw, _ := json.Marshal(resources)
	return string(raw)
}

func containsDeployment(deployments []deploymentAPIResponse, id string) bool {
	for _, deployment := range deployments {
		if deployment.ID == id {
			return true
		}
	}
	return false
}

func containsDeploymentRun(runs []deploymentRunAPIResponse, id string) bool {
	for _, run := range runs {
		if run.ID == id {
			return true
		}
	}
	return false
}

func cleanupDeploymentRows(t *testing.T, app *testApp, deploymentID string) {
	t.Helper()
	if _, err := app.pool.Exec(context.Background(), `delete from deployment_runs where deployment_external_id = $1`, deploymentID); err != nil {
		t.Fatalf("cleanup deployment runs: %v", err)
	}
	if _, err := app.pool.Exec(context.Background(), `delete from deployments where external_id = $1`, deploymentID); err != nil {
		t.Fatalf("cleanup deployment: %v", err)
	}
}

func applyScheduledOccurrence(ctx context.Context, database *db.DB, input db.ApplyScheduledOccurrenceInput) error {
	return database.Transaction(ctx, func(tx *yourbatis.Tx) error {
		return database.ApplyScheduledOccurrenceTx(ctx, tx, input)
	})
}

func newDeploymentScheduler(t *testing.T, app *testApp, events *webhooks.Enqueuer) *deploymentsapi.DeploymentScheduler {
	t.Helper()
	if err := deploymentsapi.MigrateRiver(context.Background(), app.db, nil); err != nil {
		t.Fatalf("migrate River: %v", err)
	}
	scheduler, err := deploymentsapi.NewDeploymentScheduler(app.db, events, nil)
	if err != nil {
		t.Fatalf("new deployment scheduler: %v", err)
	}
	return scheduler
}

func startDeploymentScheduler(t *testing.T, app *testApp, events *webhooks.Enqueuer) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	scheduler := newDeploymentScheduler(t, app, events)
	if err := scheduler.Start(ctx); err != nil {
		cancel()
		t.Fatalf("start deployment scheduler: %v", err)
	}
	return func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := scheduler.Stop(stopCtx); err != nil {
			t.Errorf("stop deployment scheduler: %v", err)
		}
	}
}

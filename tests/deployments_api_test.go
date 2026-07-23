package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
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

	t.Run("success missing beta header", func(t *testing.T) {
		resp := doDeploymentRequest(t, app, http.MethodGet, "/v1/deployments?beta=true", nil, defaultTestKey, false)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("failure missing beta query", func(t *testing.T) {
		resp := doDeploymentRequest(t, app, http.MethodGet, "/v1/deployments", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid json", func(t *testing.T) {
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(`{"name":`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure missing required fields", func(t *testing.T) {
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(`{"name":"missing"}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid schedule", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-bad-schedule-agent"}`)
		defer cleanupAgentRows(t, app.db, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-bad-schedule-env"}`)
		defer cleanupEnvironmentRows(t, app.db, env.ID)
		body := `{
			"agent":` + quoteJSON(agent.ID) + `,
			"environment_id":` + quoteJSON(env.ID) + `,
			"name":"bad schedule",
			"initial_events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}],
			"schedule":{"type":"cron","expression":"bad","timezone":"UTC"}
		}`
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure archived agent is rejected", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-archived-agent"}`)
		defer cleanupAgentRows(t, app.db, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-archived-agent-env"}`)
		defer cleanupEnvironmentRows(t, app.db, env.ID)
		archiveAgent(t, app, agent.ID)

		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(minimalDeploymentBody(agent.ID, env.ID)), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure archived environment is rejected", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-archived-env-agent"}`)
		defer cleanupAgentRows(t, app.db, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-archived-env"}`)
		defer cleanupEnvironmentRows(t, app.db, env.ID)
		archiveEnvironment(t, app, env.ID)

		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(minimalDeploymentBody(agent.ID, env.ID)), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure archived vault is rejected", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-archived-vault-agent"}`)
		defer cleanupAgentRows(t, app.db, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-archived-vault-env"}`)
		defer cleanupEnvironmentRows(t, app.db, env.ID)
		vault := createVault(t, app, `{"display_name":"deployments archived vault"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		archiveVault(t, app, vault.ID)

		body := deploymentBodyWithExtra(agent.ID, env.ID, `"vault_ids":[`+quoteJSON(vault.ID)+`]`)
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure deleted file resource is rejected", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-deleted-file-agent"}`)
		defer cleanupAgentRows(t, app.db, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-deleted-file-env"}`)
		defer cleanupEnvironmentRows(t, app.db, env.ID)
		file := uploadFile(t, app, "deployment-deleted-resource.txt", "text/plain", []byte("deleted resource"))
		deleteFile(t, app, file.ID)

		body := deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+`}]`)
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure list status conflicts with include archived", func(t *testing.T) {
		resp := doDeploymentRequest(t, app, http.MethodGet, "/v1/deployments?beta=true&status=active&include_archived=true", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("success manual run records reference error", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-run-error-agent"}`)
		defer cleanupAgentRows(t, app.db, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-run-error-env"}`)
		defer cleanupEnvironmentRows(t, app.db, env.ID)
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

	t.Run("success lifecycle manual run session events and run filters", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-api-agent"}`)
		defer cleanupAgentRows(t, app.db, agent.ID)
		env := createEnvironment(t, app, `{"name":"deployments-api-env"}`)
		defer cleanupEnvironmentRows(t, app.db, env.ID)
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
		if created.Type != "deployment" || !strings.HasPrefix(created.ID, "dep_") || created.Status != "active" {
			t.Fatalf("unexpected created deployment: %+v", created)
		}
		if created.EnvironmentID != env.ID || created.Description != "handles orders" {
			t.Fatalf("unexpected deployment env/description: %+v", created)
		}
		assertRawContains(t, created.Metadata, `"case":"1234"`)
		assertRawContains(t, created.Schedule, `"upcoming_runs_at"`)
		assertRawContains(t, created.Resources, `"github_repository"`)
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
		assertRawContains(t, updated.Metadata, `"priority":"high"`)
		assertRawNotContains(t, updated.Metadata, `"case"`)
		if string(updated.Resources) != "[]" || string(updated.Schedule) != "null" {
			t.Fatalf("unexpected updated resources/schedule: resources=%s schedule=%s", updated.Resources, updated.Schedule)
		}

		paused := pauseDeployment(t, app, created.ID)
		if paused.Status != "paused" || !strings.Contains(string(paused.PausedReason), `"manual"`) {
			t.Fatalf("unexpected paused deployment: %+v", paused)
		}
		resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+created.ID+"/run?beta=true", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

		unpaused := unpauseDeployment(t, app, created.ID)
		if unpaused.Status != "active" || string(unpaused.PausedReason) != "null" {
			t.Fatalf("unexpected unpaused deployment: %+v", unpaused)
		}

		run := runDeployment(t, app, created.ID)
		if !strings.HasPrefix(run.ID, "drun_") || run.DeploymentID != created.ID || run.SessionID == nil || *run.SessionID == "" {
			t.Fatalf("unexpected deployment run: %+v", run)
		}
		var filesystemCount int
		if err := app.db.Pool.QueryRow(context.Background(), `
			select count(*)
			from filestore_filesystems fs
			join workspaces w on w.uuid = fs.workspace_uuid
			join sessions s on s.uuid = fs.session_uuid and s.workspace_id = w.id
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
		missingDeploymentRuns := listDeploymentRuns(t, app, "deployment_id="+url.QueryEscape("dep_missing_test"))
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
		resp = doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+created.ID+"/run?beta=true", nil, defaultTestKey, true)
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
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments?beta=true", strings.NewReader(body), defaultTestKey, true)
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
	resp := doDeploymentRequest(t, app, http.MethodGet, "/v1/deployments/"+deploymentID+"?beta=true", nil, defaultTestKey, true)
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
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deploymentID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
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
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deploymentID+"/pause?beta=true", nil, defaultTestKey, true)
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
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deploymentID+"/unpause?beta=true", nil, defaultTestKey, true)
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
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deploymentID+"/archive?beta=true", nil, defaultTestKey, true)
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
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deploymentID+"/run?beta=true", nil, defaultTestKey, true)
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
	resp := doDeploymentRequest(t, app, http.MethodGet, "/v1/deployment_runs/"+runID+"?beta=true", nil, defaultTestKey, true)
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
	path := "/v1/deployments?beta=true"
	if query != "" {
		path += "&" + query
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
	path := "/v1/deployment_runs?beta=true"
	if query != "" {
		path += "&" + query
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
	if _, err := app.db.Pool.Exec(context.Background(), `delete from deployment_runs where deployment_external_id = $1`, deploymentID); err != nil {
		t.Fatalf("cleanup deployment runs: %v", err)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `delete from deployments where external_id = $1`, deploymentID); err != nil {
		t.Fatalf("cleanup deployment: %v", err)
	}
}

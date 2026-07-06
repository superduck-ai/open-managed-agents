package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/google/uuid"
)

type environmentAPIResponse struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Config      json.RawMessage `json:"config"`
	Metadata    json.RawMessage `json:"metadata"`
	Scope       *string         `json:"scope"`
	State       string          `json:"state"`
	ArchivedAt  *string         `json:"archived_at"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type environmentPageAPIResponse struct {
	Data     []environmentAPIResponse `json:"data"`
	NextPage *string                  `json:"next_page"`
}

type environmentWorkAPIResponse struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	EnvironmentID     string          `json:"environment_id"`
	Data              json.RawMessage `json:"data"`
	Metadata          json.RawMessage `json:"metadata"`
	State             string          `json:"state"`
	AcknowledgedAt    *string         `json:"acknowledged_at"`
	StartedAt         *string         `json:"started_at"`
	LatestHeartbeatAt *string         `json:"latest_heartbeat_at"`
	StopRequestedAt   *string         `json:"stop_requested_at"`
	StoppedAt         *string         `json:"stopped_at"`
}

type environmentWorkPageAPIResponse struct {
	Data     []environmentWorkAPIResponse `json:"data"`
	NextPage *string                      `json:"next_page"`
}

type environmentHeartbeatAPIResponse struct {
	Type          string `json:"type"`
	LastHeartbeat string `json:"last_heartbeat"`
	LeaseExtended bool   `json:"lease_extended"`
	State         string `json:"state"`
	TTLSeconds    int    `json:"ttl_seconds"`
}

type environmentWorkStatsAPIResponse struct {
	Type           string  `json:"type"`
	Depth          int     `json:"depth"`
	Pending        int     `json:"pending"`
	OldestQueuedAt *string `json:"oldest_queued_at"`
	WorkersPolling *int    `json:"workers_polling"`
}

func TestEnvironmentsAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("environments-bucket"))
	defer app.close()

	t.Run("success missing beta header", func(t *testing.T) {
		resp := doEnvironmentRequest(t, app, http.MethodGet, "/v1/environments?beta=true", nil, defaultTestKey, false)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("failure missing beta query", func(t *testing.T) {
		resp := doEnvironmentRequest(t, app, http.MethodGet, "/v1/environments", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid config type", func(t *testing.T) {
		resp := doEnvironmentRequest(t, app, http.MethodPost, "/v1/environments?beta=true", strings.NewReader(`{"name":"bad","config":{"type":"unknown"}}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid limited host", func(t *testing.T) {
		body := `{"name":"bad-host","config":{"type":"cloud","networking":{"type":"limited","allowed_hosts":["https://example.com"]}}}`
		resp := doEnvironmentRequest(t, app, http.MethodPost, "/v1/environments?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("success create update archive list delete and work lifecycle", func(t *testing.T) {
		first := createEnvironment(t, app, `{"name":"env-api-default"}`)
		defer cleanupEnvironmentRows(t, app.db, first.ID)
		if first.Type != "environment" || first.Description != "" {
			t.Fatalf("unexpected default environment: %+v", first)
		}
		if first.State != "active" {
			t.Fatalf("default environment state = %s, want active", first.State)
		}
		if first.Scope == nil || *first.Scope != "organization" {
			t.Fatalf("default environment scope = %v, want organization", first.Scope)
		}
		assertRawContains(t, first.Config, `"type":"cloud"`)
		assertRawContains(t, first.Config, `"packages"`)
		assertRawContains(t, first.Config, `"networking"`)
		assertRawContains(t, first.Config, `"init_script":""`)
		assertRawContains(t, first.Config, `"environment":{}`)
		assertRawContains(t, first.Config, `"allowed_hosts":[]`)
		assertRawContains(t, first.Config, `"allow_mcp_servers":false`)
		assertRawContains(t, first.Config, `"allow_package_managers":false`)

		scope := "organization"
		configured := createEnvironment(t, app, `{
			"name":"env-api-configured",
			"description":"configured",
			"metadata":{"a":"b"},
			"scope":"organization",
			"config":{
				"type":"cloud",
				"packages":{"pip":["numpy"],"npm":["typescript"]},
				"networking":{"type":"limited","allowed_hosts":["*.example.com"],"allow_package_managers":true}
			}
		}`)
		defer cleanupEnvironmentRows(t, app.db, configured.ID)
		if configured.Scope == nil || *configured.Scope != scope {
			t.Fatalf("configured scope = %v, want organization", configured.Scope)
		}
		assertRawContains(t, configured.Config, `"pip":["numpy"]`)
		assertRawContains(t, configured.Config, `"allowed_hosts":["*.example.com"]`)

		updated := updateEnvironment(t, app, configured.ID, `{
			"name":"env-api-configured-v2",
			"description":null,
			"metadata":{"a":"","c":"d"},
			"config":{"type":"cloud","packages":{"apt":["git"]},"networking":null}
		}`, http.StatusOK)
		if updated.Name != "env-api-configured-v2" || updated.Description != "" {
			t.Fatalf("unexpected updated environment: %+v", updated)
		}
		assertRawContains(t, updated.Metadata, `"c":"d"`)
		assertRawNotContains(t, updated.Metadata, `"a"`)
		assertRawContains(t, updated.Config, `"apt":["git"]`)
		assertRawContains(t, updated.Config, `"type":"unrestricted"`)

		page1 := listEnvironments(t, app, "limit=1")
		if len(page1.Data) != 1 || page1.NextPage == nil {
			t.Fatalf("unexpected first environments page: %+v", page1)
		}
		page2 := listEnvironments(t, app, "limit=1&page="+url.QueryEscape(*page1.NextPage))
		if len(page2.Data) != 1 {
			t.Fatalf("unexpected second environments page: %+v", page2)
		}

		archived := archiveEnvironment(t, app, first.ID)
		if archived.ArchivedAt == nil {
			t.Fatalf("archived_at = nil")
		}
		if archived.State != "archived" {
			t.Fatalf("archived state = %s, want archived", archived.State)
		}
		defaultPage := listEnvironments(t, app, "")
		if containsEnvironment(defaultPage.Data, first.ID) {
			t.Fatalf("default list included archived environment: %+v", defaultPage.Data)
		}
		archivedPage := listEnvironments(t, app, "include_archived=true")
		if !containsEnvironment(archivedPage.Data, first.ID) {
			t.Fatalf("include_archived list missing environment: %+v", archivedPage.Data)
		}

		record, err := app.db.GetEnvironment(context.Background(), getDefaultDBIDs(t, app.db).WorkspaceID, configured.ID)
		if err != nil {
			t.Fatalf("get environment db record: %v", err)
		}
		envKey := "sk-ant-env-test"
		if err := app.db.CreateEnvironmentKey(context.Background(), db.EnvironmentKey{
			ExternalID:            "envkey_test",
			OrganizationID:        record.OrganizationID,
			WorkspaceID:           record.WorkspaceID,
			EnvironmentID:         record.ID,
			EnvironmentExternalID: record.ExternalID,
		}, auth.HashAPIKey(envKey)); err != nil {
			t.Fatalf("create environment key: %v", err)
		}
		workID := createEnvironmentWork(t, app, record)
		defer cleanupEnvironmentWorkRows(t, app.db, workID)

		polled := pollEnvironmentWork(t, app, configured.ID, envKey)
		if polled.ID != workID || polled.State != "queued" {
			t.Fatalf("unexpected polled work: %+v", polled)
		}
		acked := postEnvironmentWork(t, app, configured.ID, workID, "ack", nil, envKey)
		if acked.State != "starting" || acked.AcknowledgedAt == nil || acked.StartedAt == nil {
			t.Fatalf("unexpected acked work: %+v", acked)
		}
		heartbeat := heartbeatEnvironmentWork(t, app, configured.ID, workID, "NO_HEARTBEAT", envKey)
		if heartbeat.Type != "work_heartbeat" || heartbeat.State != "active" || heartbeat.LastHeartbeat == "" || heartbeat.TTLSeconds != 60 {
			t.Fatalf("unexpected heartbeat: %+v", heartbeat)
		}
		staleResp := doEnvironmentBearerRequest(t, app, http.MethodPost, "/v1/environments/"+configured.ID+"/work/"+workID+"/heartbeat?beta=true&expected_last_heartbeat=wrong", nil, envKey, true)
		assertError(t, staleResp, http.StatusPreconditionFailed, "invalid_request_error")

		updatedWork := updateEnvironmentWork(t, app, configured.ID, workID, `{"metadata":{"state":"seen"}}`, envKey)
		assertRawContains(t, updatedWork.Metadata, `"state":"seen"`)
		workPage := listEnvironmentWork(t, app, configured.ID, envKey)
		if len(workPage.Data) != 1 || workPage.Data[0].ID != workID {
			t.Fatalf("unexpected work page: %+v", workPage)
		}
		stats := environmentWorkStats(t, app, configured.ID, envKey)
		if stats.Type != "work_queue_stats" || stats.Pending != 1 || stats.WorkersPolling == nil || *stats.WorkersPolling != 1 {
			t.Fatalf("unexpected work stats: %+v", stats)
		}
		stopped := postEnvironmentWork(t, app, configured.ID, workID, "stop", strings.NewReader(`{"force":true}`), envKey)
		if stopped.State != "stopped" || stopped.StoppedAt == nil {
			t.Fatalf("unexpected stopped work: %+v", stopped)
		}

		deleteEnvironment(t, app, configured.ID)
	})
}

func TestEnvironmentsSchemaHasNoForeignKeys(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("environments-schema-bucket"))
	defer app.close()

	var foreignKeyCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from pg_constraint con
		join pg_class cls on cls.oid = con.conrelid
		join pg_namespace ns on ns.oid = cls.relnamespace
		where con.contype = 'f'
			and ns.oid = current_schema()::regnamespace
			and cls.relname in ('environments', 'environment_keys', 'environment_work', 'environment_worker_polls', 'environment_sandboxes')
	`).Scan(&foreignKeyCount); err != nil {
		t.Fatalf("count environments foreign keys: %v", err)
	}
	if foreignKeyCount != 0 {
		t.Fatalf("environments foreign key count = %d, want 0", foreignKeyCount)
	}
}

func TestEnvironmentsOfficialSDKFixture(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("environments-fixture-bucket"))
	defer app.close()

	resp := doEnvironmentRequest(t, app, http.MethodGet, "/v1/environments/"+app.cfg.OfficialSDKFixtureEnvironmentID+"?beta=true", nil, config.OfficialSDKResourceAPIKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fixture environment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var env environmentAPIResponse
	decodeJSON(t, resp.Body, &env)
	if env.ID != app.cfg.OfficialSDKFixtureEnvironmentID {
		t.Fatalf("unexpected fixture environment: %+v", env)
	}
	if env.State != "active" {
		t.Fatalf("fixture state = %s, want active", env.State)
	}
	if env.Scope == nil || *env.Scope != "organization" {
		t.Fatalf("fixture scope = %v, want organization", env.Scope)
	}
	assertRawContains(t, env.Config, `"init_script":""`)
	assertRawContains(t, env.Config, `"environment":{}`)
}

func doEnvironmentRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, betaHeader bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new environment request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if betaHeader {
		req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do environment request: %v", err)
	}
	return resp
}

func doEnvironmentBearerRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, betaHeader bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new environment bearer request: %v", err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Anthropic-Worker-ID", "worker-test")
	if betaHeader {
		req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do environment bearer request: %v", err)
	}
	return resp
}

func createEnvironment(t *testing.T, app *testApp, body string) environmentAPIResponse {
	t.Helper()
	resp := doEnvironmentRequest(t, app, http.MethodPost, "/v1/environments?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create environment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var env environmentAPIResponse
	decodeJSON(t, resp.Body, &env)
	if env.ID == "" {
		t.Fatalf("create environment returned empty id: %+v", env)
	}
	return env
}

func updateEnvironment(t *testing.T, app *testApp, envID, body string, wantStatus int) environmentAPIResponse {
	t.Helper()
	resp := doEnvironmentRequest(t, app, http.MethodPost, "/v1/environments/"+envID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("update environment status = %d, want %d: %s", resp.StatusCode, wantStatus, readAll(t, resp.Body))
	}
	var env environmentAPIResponse
	if wantStatus == http.StatusOK {
		decodeJSON(t, resp.Body, &env)
	}
	return env
}

func listEnvironments(t *testing.T, app *testApp, query string) environmentPageAPIResponse {
	t.Helper()
	path := "/v1/environments?beta=true"
	if query != "" {
		path += "&" + query
	}
	resp := doEnvironmentRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list environments status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page environmentPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func archiveEnvironment(t *testing.T, app *testApp, envID string) environmentAPIResponse {
	t.Helper()
	resp := doEnvironmentRequest(t, app, http.MethodPost, "/v1/environments/"+envID+"/archive?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive environment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var env environmentAPIResponse
	decodeJSON(t, resp.Body, &env)
	return env
}

func deleteEnvironment(t *testing.T, app *testApp, envID string) {
	t.Helper()
	resp := doEnvironmentRequest(t, app, http.MethodDelete, "/v1/environments/"+envID+"?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete environment status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func pollEnvironmentWork(t *testing.T, app *testApp, envID, envKey string) environmentWorkAPIResponse {
	t.Helper()
	resp := doEnvironmentBearerRequest(t, app, http.MethodGet, "/v1/environments/"+envID+"/work/poll?beta=true&block_ms=1", nil, envKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("poll work status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var work environmentWorkAPIResponse
	decodeJSON(t, resp.Body, &work)
	return work
}

func postEnvironmentWork(t *testing.T, app *testApp, envID, workID, action string, body io.Reader, envKey string) environmentWorkAPIResponse {
	t.Helper()
	resp := doEnvironmentBearerRequest(t, app, http.MethodPost, "/v1/environments/"+envID+"/work/"+workID+"/"+action+"?beta=true", body, envKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s work status = %d, want 200: %s", action, resp.StatusCode, readAll(t, resp.Body))
	}
	var work environmentWorkAPIResponse
	decodeJSON(t, resp.Body, &work)
	return work
}

func heartbeatEnvironmentWork(t *testing.T, app *testApp, envID, workID, expected, envKey string) environmentHeartbeatAPIResponse {
	t.Helper()
	resp := doEnvironmentBearerRequest(t, app, http.MethodPost, "/v1/environments/"+envID+"/work/"+workID+"/heartbeat?beta=true&expected_last_heartbeat="+url.QueryEscape(expected)+"&desired_ttl_seconds=0", nil, envKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat work status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var heartbeat environmentHeartbeatAPIResponse
	decodeJSON(t, resp.Body, &heartbeat)
	return heartbeat
}

func updateEnvironmentWork(t *testing.T, app *testApp, envID, workID, body, envKey string) environmentWorkAPIResponse {
	t.Helper()
	resp := doEnvironmentBearerRequest(t, app, http.MethodPost, "/v1/environments/"+envID+"/work/"+workID+"?beta=true", strings.NewReader(body), envKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update work status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var work environmentWorkAPIResponse
	decodeJSON(t, resp.Body, &work)
	return work
}

func listEnvironmentWork(t *testing.T, app *testApp, envID, envKey string) environmentWorkPageAPIResponse {
	t.Helper()
	resp := doEnvironmentBearerRequest(t, app, http.MethodGet, "/v1/environments/"+envID+"/work?beta=true", nil, envKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list work status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page environmentWorkPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func environmentWorkStats(t *testing.T, app *testApp, envID, envKey string) environmentWorkStatsAPIResponse {
	t.Helper()
	resp := doEnvironmentBearerRequest(t, app, http.MethodGet, "/v1/environments/"+envID+"/work/stats?beta=true", nil, envKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("work stats status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var stats environmentWorkStatsAPIResponse
	decodeJSON(t, resp.Body, &stats)
	return stats
}

func createEnvironmentWork(t *testing.T, app *testApp, env db.Environment) string {
	t.Helper()
	workID, err := ids.New("work_")
	if err != nil {
		t.Fatalf("new work id: %v", err)
	}
	if _, err := app.db.CreateEnvironmentWork(context.Background(), db.EnvironmentWork{
		UUID:                  uuid.NewString(),
		ExternalID:            workID,
		OrganizationID:        env.OrganizationID,
		WorkspaceID:           env.WorkspaceID,
		EnvironmentID:         env.ID,
		EnvironmentExternalID: env.ExternalID,
		Data:                  json.RawMessage(`{"type":"session","id":"session_test"}`),
		Metadata:              json.RawMessage(`{}`),
		State:                 "queued",
		CreatedAt:             time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create environment work: %v", err)
	}
	return workID
}

func containsEnvironment(environments []environmentAPIResponse, id string) bool {
	for _, env := range environments {
		if env.ID == id {
			return true
		}
	}
	return false
}

func cleanupEnvironmentRows(t *testing.T, database *db.DB, envID string) {
	t.Helper()
	if _, err := database.Pool.Exec(context.Background(), `delete from environment_sandboxes where environment_external_id = $1`, envID); err != nil {
		t.Fatalf("cleanup environment sandboxes: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `delete from environment_work where environment_external_id = $1`, envID); err != nil {
		t.Fatalf("cleanup environment work: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `delete from environment_worker_polls where environment_external_id = $1`, envID); err != nil {
		t.Fatalf("cleanup environment worker polls: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `delete from environment_keys where environment_external_id = $1`, envID); err != nil {
		t.Fatalf("cleanup environment keys: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `delete from environments where external_id = $1`, envID); err != nil {
		t.Fatalf("cleanup environment: %v", err)
	}
}

func cleanupEnvironmentWorkRows(t *testing.T, database *db.DB, workID string) {
	t.Helper()
	if _, err := database.Pool.Exec(context.Background(), `delete from environment_sandboxes where work_external_id = $1`, workID); err != nil {
		t.Fatalf("cleanup work sandboxes: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `delete from environment_work where external_id = $1`, workID); err != nil {
		t.Fatalf("cleanup work: %v", err)
	}
}

//go:build integration || e2e || e2b_integration

package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAgentOptionalUpdateVersion(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("agent-optional-version"))
	defer app.close()
	created := createAgent(t, app, `{"model":"claude-opus-4-6","name":"optional-version","system":"original"}`)
	defer cleanupAgentRows(t, app.pool, created.ID)
	for _, version := range []string{"null", "0", "-1", `"1"`} {
		resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents/"+created.ID+"?beta=true", strings.NewReader(`{"version":`+version+`,"name":"invalid"}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	}
	updated := updateAgent(t, app, created.ID, `{"name":"unconditional"}`, http.StatusOK)
	if updated.Version != 2 || updated.Name != "unconditional" || updated.System == nil || *updated.System != "original" {
		t.Fatalf("unconditional update lost fields: %+v", updated)
	}
	resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents/"+created.ID+"?beta=true", strings.NewReader(`{"version":1,"name":"stale"}`), defaultTestKey, true)
	assertError(t, resp, http.StatusConflict, "conflict_error")
	noop := updateAgent(t, app, created.ID, `{"name":"unconditional"}`, http.StatusOK)
	if noop.Version != 2 {
		t.Fatalf("no-op created version %d", noop.Version)
	}
	updated = updateAgent(t, app, created.ID, `{"system":"latest"}`, http.StatusOK)
	if updated.Version != 3 || updated.Name != "unconditional" || updated.System == nil || *updated.System != "latest" {
		t.Fatalf("successive unconditional update lost fields: %+v", updated)
	}
	archiveAgent(t, app, created.ID)
	resp = doAgentRequest(t, app, http.MethodPost, "/v1/agents/"+created.ID+"?beta=true", strings.NewReader(`{"name":"archived"}`), defaultTestKey, true)
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
}

func TestAgentConcurrentUnconditionalUpdatesPreserveIndependentFields(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("agent-optional-version-concurrent"))
	defer app.close()
	created := createAgent(t, app, `{"model":"claude-opus-4-6","name":"before","system":"before"}`)
	defer cleanupAgentRows(t, app.pool, created.ID)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, body := range []string{`{"name":"after"}`, `{"system":"after"}`} {
		go func() {
			<-start
			req, err := http.NewRequest(http.MethodPost, app.server.URL+"/v1/agents/"+created.ID+"?beta=true", strings.NewReader(body))
			if err != nil {
				results <- err
				return
			}
			req.Header.Set("x-api-key", defaultTestKey)
			req.Header.Set("anthropic-version", "2023-06-01")
			req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.server.Client().Do(req)
			if err != nil {
				results <- err
				return
			}
			defer resp.Body.Close()
			var result agentAPIResponse
			err = json.NewDecoder(resp.Body).Decode(&result)
			if resp.StatusCode != http.StatusOK {
				err = fmt.Errorf("update status %d", resp.StatusCode)
			}
			results <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	got := retrieveAgent(t, app, created.ID, "")
	if got.Version != 3 || got.Name != "after" || got.System == nil || *got.System != "after" {
		t.Fatalf("concurrent unconditional update lost fields: %+v", got)
	}
}

func TestWebhookAgentOptionalUpdateVersion(t *testing.T) {
	app, _, _ := newEventSubscription(t, agentWebhookEvents)
	agent := newWebhookAgent(t, app)
	updated := updateAgent(t, app, agent.ID, `{"name":"without-version"}`, http.StatusOK)
	if updated.Version != 2 {
		t.Fatalf("version = %d, want 2", updated.Version)
	}
	assertAgentWebhookCounts(t, app, agent.ID, 1, 1, 0)
	updateAgent(t, app, agent.ID, `{"name":"without-version"}`, http.StatusOK)
	assertAgentWebhookCounts(t, app, agent.ID, 1, 1, 0)
	resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents/"+agent.ID+"?beta=true", strings.NewReader(`{"version":1,"name":"stale"}`), defaultTestKey, true)
	assertError(t, resp, http.StatusConflict, "conflict_error")
	assertAgentWebhookCounts(t, app, agent.ID, 1, 1, 0)
}

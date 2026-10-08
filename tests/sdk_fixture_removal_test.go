package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

// This identity used to bypass validation and persistence in production handlers.
// Register it explicitly in the test database to prove that it is now ordinary.
const formerSDKKey = "my-anthropic-api-key"

func newFormerSDKIdentityApp(t *testing.T) *testApp {
	t.Helper()
	app := newPayloadIntegrationApp(t, newFakeStore("sdk-real-resources"))
	if err := app.db.Seed(t.Context(), []config.SeedAPIKey{{ExternalID: "api_key_official_sdk_resource_tests", Key: formerSDKKey}}); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestFormerSDKIdentityCannotRetrieveMissingResources(t *testing.T) {
	app := newFormerSDKIdentityApp(t)
	for _, path := range []string{
		"/v1/agents/agent_011CZkYpogX7uDKUyvBTophP",
		"/v1/environments/env_011CZkZ9X2dpNyB7HsEFoRfW",
		"/v1/files/file_id",
		"/v1/skills/skill_id",
		"/v1/messages/batches/message_batch_id",
		"/v1/sessions/sesn_011CZkZAtmR3yMPDzynEDxu7",
	} {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, app.baseURL+path+"?beta=true", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Api-Key", formerSDKKey)
			req.Header.Set("anthropic-version", "2023-06-01")
			req.Header.Set("anthropic-beta", "managed-agents-2026-04-01,files-api-2025-04-14,skills-2025-10-02,message-batches-2024-09-24")
			resp, err := app.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			assertError(t, resp, http.StatusNotFound, "not_found_error")
		})
	}
	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/sesn_011CZkZAtmR3yMPDzynEDxu7/events?beta=true", strings.NewReader(`{"events":[{"type":"user.message","content":[{"type":"text","text":"must not succeed"}]}]}`), formerSDKKey, true)
	assertError(t, resp, http.StatusNotFound, "not_found_error")
}

func TestFormerSDKIdentityCannotBypassValidation(t *testing.T) {
	app := newFormerSDKIdentityApp(t)
	t.Run("invalid batch", func(t *testing.T) {
		resp := doBatchRequest(t, app, http.MethodPost, "/v1/messages/batches", strings.NewReader(`{"requests":[]}`), formerSDKKey, "application/json")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})
	t.Run("invalid skill package", func(t *testing.T) {
		body, contentType := skillMultipartBody(t, "invalid-sdk-upload", []skillUploadFile{{FieldName: "files[]", Filename: "anonymous_file", Content: "Example data"}})
		resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, formerSDKKey, true, contentType)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})
	t.Run("missing session dependencies", func(t *testing.T) {
		resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions?beta=true", strings.NewReader(`{"agent":"agent_011CZkYpogX7uDKUyvBTophP","environment_id":"env_011CZkZ9X2dpNyB7HsEFoRfW"}`), formerSDKKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})
}

func TestFormerSDKIdentitySessionEventsPersistAndMatchPublicStream(t *testing.T) {
	app := newFormerSDKIdentityApp(t)
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sdk-real-agent"}`)
	env := createEnvironment(t, app, `{"name":"sdk-real-environment"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/sessions/"+session.ID+"/events/stream?beta=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", formerSDKKey)
	req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	sent := sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"persisted SDK input"}]}]}`, formerSDKKey)
	if len(sent.Data) != 1 {
		t.Fatalf("send response = %+v", sent)
	}
	decode := func(raw []byte) map[string]any {
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	expected := decode(sent.Data[0])
	if expected["id"] == "" || expected["id"] == nil {
		t.Fatal("missing persisted event ID")
	}
	history := listSessionEvents(t, app, session.ID, "types[]=user.message", formerSDKKey)
	if len(history.Data) != 1 || !reflect.DeepEqual(decode(history.Data[0]), expected) {
		t.Fatalf("history differs from send response: %+v", history)
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		value := decode([]byte(strings.TrimPrefix(line, "data: ")))
		if value["id"] != expected["id"] {
			continue
		}
		// SSE supplies the primary thread owner in addition to the session-level payload.
		var threadID string
		if err := app.pool.QueryRow(t.Context(), `SELECT external_id FROM session_threads WHERE session_external_id = $1 AND parent_thread_uuid IS NULL`, session.ID).Scan(&threadID); err != nil {
			t.Fatal(err)
		}
		expected["session_thread_id"] = threadID
		if !reflect.DeepEqual(value, expected) {
			t.Fatalf("stream differs from persisted event: %+v", value)
		}
		return
	}
	t.Fatalf("stream ended before persisted event: %v", scanner.Err())
}

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

const messagesTestModel = "claude-opus-4-8"

func TestMessagesAPIUsesCompatibleProviderAuthentication(t *testing.T) {
	upstreamHeaders := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","content":[]}`)
	}))
	defer upstream.Close()

	app := newTestAppWithStore(t, nil, newFakeStore("messages-provider-auth-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "Messages auth", upstream.URL, "server-provider-key", messagesTestModel)

	response := doMessagesRequest(
		t,
		app,
		defaultTestKey,
		`{"model":"`+messagesTestModel+`","max_tokens":16,"messages":[]}`,
	)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Messages status = %d, want 200: %s", response.StatusCode, readAll(t, response.Body))
	}

	headers := <-upstreamHeaders
	if headers.Get("X-Api-Key") != "server-provider-key" || headers.Get("Authorization") != "Bearer server-provider-key" {
		t.Fatalf("provider authentication headers = %#v", headers)
	}
}

func TestMessagesAPIFailures(t *testing.T) {
	t.Run("failure workspace Provider is required", func(t *testing.T) {
		app := newTestAppWithStore(t, nil, newFakeStore("messages-no-provider-bucket"))
		defer app.close()
		clearTestLLMProviders(t, app)

		resp := doMessagesRequest(t, app, defaultTestKey, `{"model":"`+messagesTestModel+`","max_tokens":16,"messages":[]}`)
		assertError(t, resp, http.StatusServiceUnavailable, "api_error")
	})

	app := newTestAppWithStore(t, nil, newFakeStore("messages-failures-bucket"))
	defer app.close()

	t.Run("failure model is not configured", func(t *testing.T) {
		resp := doMessagesRequest(t, app, defaultTestKey, `{"model":"not-configured","max_tokens":16,"messages":[]}`)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure credential issue rejects mismatched tenant", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		_, err := app.db.GetCodeSessionCredentialContextForIssue(
			context.Background(),
			uuid.NewV4().String(),
			credential.WorkspaceUUID,
			credential.CodeSessionID,
		)
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("issue credential with mismatched organization error = %v, want ErrNotFound", err)
		}
		_, err = app.db.GetCodeSessionCredentialContextForIssue(
			context.Background(),
			credential.OrganizationUUID,
			uuid.NewV4().String(),
			credential.CodeSessionID,
		)
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("issue credential with mismatched workspace error = %v, want ErrNotFound", err)
		}
	})

	t.Run("failure session credential cannot access other resources", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		req, err := http.NewRequest(http.MethodGet, app.baseURL+"/v1/models", nil)
		if err != nil {
			t.Fatalf("new models request: %v", err)
		}
		req.Header.Set("X-Api-Key", credential.Token)
		resp, err := app.client.Do(req)
		if err != nil {
			t.Fatalf("do models request: %v", err)
		}
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure unregistered credential is rejected but can register", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		resp := doMessagesRequest(t, app, credential.Token, `{"model":"`+messagesTestModel+`","max_tokens":16,"messages":[]}`)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
		if epoch := registerCodeSessionWorker(t, app, credential.CodeSessionID); epoch != "1" {
			t.Fatalf("initial worker epoch = %q, want 1", epoch)
		}
	})

	t.Run("failure expired worker lease rejects credential", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		registerCodeSessionWorker(t, app, credential.CodeSessionID)
		if _, err := app.pool.Exec(context.Background(), `
			update code_sessions
			set worker_lease_expires_at = now() - interval '1 minute'
			where external_id = $1
		`, credential.CodeSessionID); err != nil {
			t.Fatalf("expire Messages credential worker lease: %v", err)
		}
		resp := doMessagesRequest(t, app, credential.Token, `{"model":"`+messagesTestModel+`","max_tokens":16,"messages":[]}`)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure terminated public session rejects credential", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		registerCodeSessionWorker(t, app, credential.CodeSessionID)
		var previousStatus string
		if err := app.pool.QueryRow(context.Background(), `select status from sessions where uuid = $1`, credential.PublicSessionUUID).Scan(&previousStatus); err != nil {
			t.Fatalf("load public session status: %v", err)
		}
		t.Cleanup(func() {
			_, _ = app.pool.Exec(context.Background(), `update sessions set status = $2 where uuid = $1`, credential.PublicSessionUUID, previousStatus)
		})
		if _, err := app.pool.Exec(context.Background(), `update sessions set status = 'terminated' where uuid = $1`, credential.PublicSessionUUID); err != nil {
			t.Fatalf("terminate public session: %v", err)
		}
		resp := doMessagesRequest(t, app, credential.Token, `{"model":"`+messagesTestModel+`","max_tokens":16,"messages":[]}`)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure removed bridge endpoint rejects session credential", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		resp := doSessionBearerRequest(t, app, http.MethodPost, "/v1/code/sessions/"+credential.CodeSessionID+"/bridge", strings.NewReader(`{}`), credential.Token, false)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure removed bridge endpoint is not found for workspace credential", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		resp := doSessionBearerRequest(t, app, http.MethodPost, "/v1/code/sessions/"+credential.CodeSessionID+"/bridge", strings.NewReader(`{}`), defaultTestKey, false)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})
}

type messagesCodeSessionCredential struct {
	Token             string
	CodeSessionID     string
	PublicSessionUUID string
	OrganizationUUID  string
	WorkspaceUUID     string
}

func createMessagesCodeSessionCredential(t *testing.T, app *testApp, model string) messagesCodeSessionCredential {
	t.Helper()
	apiKey, err := app.db.GetAPIKey(context.Background(), auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatalf("load default API key: %v", err)
	}
	token, err := ids.New("sk-ant-oat01-test-")
	if err != nil {
		t.Fatalf("generate Messages access token: %v", err)
	}
	codeSessionID, err := ids.New("cse_messages_test_")
	if err != nil {
		t.Fatalf("generate code session ID: %v", err)
	}
	var sessionUUID, sessionExternalID, environmentUUID string
	if err := app.pool.QueryRow(context.Background(), `
		select uuid::text, external_id, environment_uuid::text
		from sessions
		where workspace_uuid = $1 and organization_uuid = $2 and deleted_at is null
		order by uuid
		limit 1
	`, apiKey.WorkspaceUUID, apiKey.OrganizationUUID).Scan(&sessionUUID, &sessionExternalID, &environmentUUID); err != nil {
		t.Fatalf("load Messages credential public session: %v", err)
	}
	now := time.Now().UTC()
	_, err = app.db.CreateCodeSession(context.Background(), db.CreateCodeSessionInput{
		ExternalID:            codeSessionID,
		OrganizationUUID:      apiKey.OrganizationUUID.String(),
		WorkspaceUUID:         apiKey.WorkspaceUUID.String(),
		SessionUUID:           sessionUUID,
		SessionExternalID:     sessionExternalID,
		EnvironmentUUID:       environmentUUID,
		EnvironmentExternalID: "environment_" + codeSessionID,
		PermissionMode:        "bypassPermissions",
		Model:                 model,
		Status:                "active",
		Metadata:              json.RawMessage(`{}`),
		OAuthAccessTokenHash:  auth.HashAPIKey(token),
		CreatedAt:             now,
	})
	if err != nil {
		t.Fatalf("create code session: %v", err)
	}
	return messagesCodeSessionCredential{
		Token:             token,
		CodeSessionID:     codeSessionID,
		PublicSessionUUID: sessionUUID,
		OrganizationUUID:  apiKey.OrganizationUUID.String(),
		WorkspaceUUID:     apiKey.WorkspaceUUID.String(),
	}
}

func doMessagesRequest(t *testing.T, app *testApp, apiKey string, payload string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/messages", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("new messages request: %v", err)
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do messages request: %v", err)
	}
	return resp
}

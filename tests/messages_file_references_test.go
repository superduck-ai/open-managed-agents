//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMessagesManagedFileReferences(t *testing.T) {
	var calls atomic.Int64
	bodies := make(chan string, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies <- string(b)
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","content":[]}`)
	}))
	defer upstream.Close()
	app := newTestAppWithStore(t, nil, newFakeStore("managed-model-files"))
	defer app.close()
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "file-reference-provider", upstream.URL, "fixture-provider-key", messagesTestModel)
	agent := createAgent(t, app, `{"name":"file-reference-agent","model":{"id":"`+messagesTestModel+`"}}`)
	defer archiveAgent(t, app, agent.ID)
	environment := createEnvironment(t, app, `{"name":"file-reference-environment","config":{"type":"cloud"}}`)
	defer archiveEnvironment(t, app, environment.ID)
	session := createSession(t, app, fmt.Sprintf(`{"agent":%q,"environment_id":%q}`, agent.ID, environment.ID))
	defer archiveSession(t, app, session.ID)
	credential := createManagedFileReferenceCredential(t, app, session.ID)
	registerCodeSessionWorker(t, app, credential.CodeSessionID)
	file := uploadFile(t, app, "reference.txt", "text/plain", []byte("编号=ACTUAL-STORED-CONTENT"))
	defer deleteFile(t, app, file.ID)
	request := func(id string) string {
		b, _ := json.Marshal(map[string]any{"model": messagesTestModel, "max_tokens": 16, "custom_parameter": "retained", "messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "document", "title": "Title", "source": map[string]string{"type": "file", "file_id": id}}}}}})
		return string(b)
	}
	t.Run("missing reference never reaches provider", func(t *testing.T) {
		before := calls.Load()
		r := doMessagesRequest(t, app, credential.Token, request("file_missing_fixture"))
		assertError(t, r, 404, "not_found_error")
		if calls.Load() != before {
			t.Fatal("missing file reached inference")
		}
	})
	t.Run("foreign reference never reaches provider", func(t *testing.T) {
		const key = "sk-ant-local-file-reference-peer"
		seedWorkspaceKey(t, app.pool, "org_file_peer", "workspace_file_peer", "api_key_file_peer", key)
		b, ct := multipartBody(t, "foreign.txt", "text/plain", []byte("foreign-material"), false)
		r := app.do(t, http.MethodPost, "/v1/files?beta=true", b, key, true, ct)
		defer r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("peer upload status%d", r.StatusCode)
		}
		var file metadataResponse
		decodeJSON(t, r.Body, &file)
		defer func() {
			r := app.do(t, http.MethodDelete, "/v1/files/"+file.ID+"?beta=true", nil, key, true, "")
			r.Body.Close()
		}()
		before := calls.Load()
		response := doMessagesRequest(t, app, credential.Token, request(file.ID))
		assertError(t, response, 404, "not_found_error")
		if calls.Load() != before {
			t.Fatal("foreign file reached inference")
		}
	})
	t.Run("oversized metadata rejected before storage read", func(t *testing.T) {
		if _, e := app.pool.Exec(context.Background(), "update files set size_bytes=$1 where external_id=$2", llmproviders.MaxMessagesRequestBodyBytes+1, file.ID); e != nil {
			t.Fatal(e)
		}
		defer func() {
			if _, e := app.pool.Exec(context.Background(), "update files set size_bytes=$1 where external_id=$2", len("编号=ACTUAL-STORED-CONTENT"), file.ID); e != nil {
				t.Error(e)
			}
		}()
		before := calls.Load()
		r := doMessagesRequest(t, app, credential.Token, request(file.ID))
		assertError(t, r, 400, "invalid_request_error")
		if calls.Load() != before {
			t.Fatal("oversized file reached inference")
		}
	})
	t.Run("invalid UTF8 text rejected before inference", func(t *testing.T) {
		invalid := uploadFile(t, app, "invalid.txt", "text/plain", []byte{255, 254})
		defer deleteFile(t, app, invalid.ID)
		before := calls.Load()
		r := doMessagesRequest(t, app, credential.Token, request(invalid.ID))
		assertError(t, r, 400, "invalid_request_error")
		if calls.Load() != before {
			t.Fatal("invalid text reached inference")
		}
	})
	t.Run("deleted reference never reaches provider", func(t *testing.T) {
		deleted := uploadFile(t, app, "deleted-reference.txt", "text/plain", []byte("previously-readable"))
		defer deleteFile(t, app, deleted.ID)
		warm := doMessagesRequest(t, app, credential.Token, request(deleted.ID))
		if warm.StatusCode != 200 {
			warm.Body.Close()
			t.Fatalf("positive cache control status%d", warm.StatusCode)
		}
		warm.Body.Close()
		if body := <-bodies; !strings.Contains(body, "previously-readable") {
			t.Fatal("positive file precondition failed")
		}
		deleteFile(t, app, deleted.ID)
		before := calls.Load()
		r := doMessagesRequest(t, app, credential.Token, request(deleted.ID))
		assertError(t, r, 404, "not_found_error")
		if calls.Load() != before {
			t.Fatal("deleted file reached inference")
		}
	})
	t.Run("active bytes resolved before inference", func(t *testing.T) {
		r := doMessagesRequest(t, app, credential.Token, request(file.ID))
		defer r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("valid reference status%d: %s", r.StatusCode, readAll(t, r.Body))
		}
		body := <-bodies
		if !strings.Contains(body, "ACTUAL-STORED-CONTENT") || strings.Contains(body, file.ID) || !strings.Contains(body, `"custom_parameter":"retained"`) {
			t.Fatalf("upstream did not receive resolved data with other fields intact: %s", body)
		}
	})
	t.Run("same workspace file scope metadata remains reusable", func(t *testing.T) {
		if _, e := app.pool.Exec(context.Background(), "update files set scope_type='session',scope_id='session_other_fixture' where external_id=$1", file.ID); e != nil {
			t.Fatal(e)
		}
		defer func() {
			if _, e := app.pool.Exec(context.Background(), "update files set scope_type=null,scope_id=null where external_id=$1", file.ID); e != nil {
				t.Error(e)
			}
		}()
		r := doMessagesRequest(t, app, credential.Token, request(file.ID))
		defer r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("explicit same-workspace file status%d", r.StatusCode)
		}
		if body := <-bodies; !strings.Contains(body, "ACTUAL-STORED-CONTENT") {
			t.Fatal("scope metadata incorrectly prevented valid workspace file resolution")
		}
	})

	t.Run("ordinary provider proxy stays opaque", func(t *testing.T) {
		original := request("file_provider_owned")
		r := doMessagesRequest(t, app, defaultTestKey, original)
		defer r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("opaque proxy status%d", r.StatusCode)
		}
		if body := <-bodies; body != original {
			t.Fatal("ordinary Messages request mutated")
		}
	})

}

func createManagedFileReferenceCredential(t *testing.T, app *testApp, sessionID string) messagesCodeSessionCredential {
	t.Helper()
	ctx := context.Background()
	key, err := app.db.GetAPIKey(ctx, auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := app.db.GetSession(ctx, key.WorkspaceUUID.String(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := ids.New("sk-ant-oat01-test-")
	if err != nil {
		t.Fatal(err)
	}
	id, err := ids.New("cse_file_reference_test_")
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.db.CreateCodeSession(ctx, db.CreateCodeSessionInput{ExternalID: id, OrganizationUUID: key.OrganizationUUID.String(), WorkspaceUUID: key.WorkspaceUUID.String(), SessionUUID: session.UUID, SessionExternalID: session.ExternalID, EnvironmentUUID: session.EnvironmentUUID, EnvironmentExternalID: session.EnvironmentExternalID, PermissionMode: "bypassPermissions", Model: messagesTestModel, Status: "active", Metadata: json.RawMessage(`{}`), OAuthAccessTokenHash: auth.HashAPIKey(token), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return messagesCodeSessionCredential{Token: token, CodeSessionID: id, PublicSessionUUID: session.UUID, OrganizationUUID: key.OrganizationUUID.String(), WorkspaceUUID: key.WorkspaceUUID.String()}
}

package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestCodeSessionCommitSigningRequiresCurrentWorkerEpoch(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("code-signing-api-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"code-signing-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	environment := createEnvironment(t, app, `{"name":"code-signing-environment"}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	if epoch := registerCodeSessionWorker(t, app, codeSessionID); epoch != "1" {
		t.Fatalf("initial worker epoch = %q, want 1", epoch)
	}
	staleToken := codeSessionIngressToken(t, app, codeSessionID)
	if _, err := app.pool.Exec(
		context.Background(),
		`update code_sessions set current_worker_epoch = current_worker_epoch + 1 where external_id = $1`,
		codeSessionID,
	); err != nil {
		t.Fatalf("advance worker epoch: %v", err)
	}

	t.Run("failure stale epoch", func(t *testing.T) {
		response := requestCommitSignature(t, app, codeSessionID, staleToken, `{"contents":"commit payload"}`)
		assertError(t, response, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("success current epoch", func(t *testing.T) {
		body := `{"contents":"tree abcdef\n\nSigned through OMA\n","source":{"type":"git_repository","git_info":{"type":"github","repo":"superduck-ai/open-managed-agents"}},"git_object_format":"sha1"}`
		response := requestCommitSignature(t, app, codeSessionID, codeSessionIngressToken(t, app, codeSessionID), body)
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.StatusCode, http.StatusOK, readAll(t, response.Body))
		}
		var decoded struct {
			Signature string `json:"signature"`
		}
		decodeJSON(t, response.Body, &decoded)
		if !strings.HasPrefix(decoded.Signature, "-----BEGIN SSH SIGNATURE-----\n") || !strings.HasSuffix(decoded.Signature, "-----END SSH SIGNATURE-----\n") {
			t.Fatalf("unexpected signature response: %q", decoded.Signature)
		}
	})
}

func requestCommitSignature(t *testing.T, app *testApp, codeSessionID, token, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(
		http.MethodPost,
		app.baseURL+"/v1/code/sessions/"+codeSessionID+"/sign-commit",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("new commit signing request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatalf("request commit signature: %v", err)
	}
	return response
}

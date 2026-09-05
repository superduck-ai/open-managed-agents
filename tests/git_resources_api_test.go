package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func TestGitResourcesAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("git-resources-api"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"git-resource-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	environment := createEnvironment(t, app, `{"name":"git-resource-environment"}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)
	base := `"agent":` + quoteJSON(agent.ID) + `,"environment_id":` + quoteJSON(environment.ID)
	resource := `{"type":"github_repository","url":"https://github.com/example/repository","authorization_token":"session-github-secret","checkout":{"type":"branch","name":"feature/resource"}}`

	t.Run("failure invalid inputs in both creation APIs", func(t *testing.T) {
		cases := []string{
			`[{"type":"github_repository","url":"https://github.com/example/repository","authorization_token":42}]`,
			`[null]`,
			`[{"type":"github_repository","url":"https://github.com/example/repository.git","authorization_token":"secret"}]`,
			`[{"type":"github_repository","url":"https://github.com/example/repository","authorization_token":"secret","mount_path":"/workspace/../etc"}]`,
			`[{"type":"github_repository","url":"https://github.com/example/repository","authorization_token":"secret","checkout":{"type":"commit","sha":"--upload-pack=x"}}]`,
			`[` + resource + `,` + strings.ReplaceAll(resource, `"https://github.com/example/repository"`, `"https://github.com/Example/Repository"`) + `]`,
		}
		for _, resources := range cases {
			response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions?beta=true", strings.NewReader(`{`+base+`,"resources":`+resources+`}`), defaultTestKey, true)
			assertError(t, response, http.StatusBadRequest, "invalid_request_error")
			response = doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments", strings.NewReader(deploymentBodyWithExtra(agent.ID, environment.ID, `"resources":`+resources)), defaultTestKey, true)
			assertError(t, response, http.StatusBadRequest, "invalid_request_error")
		}
	})

	t.Run("success anonymous session and deployment run", func(t *testing.T) {
		publicResource := `{"type":"github_repository","url":"https://github.com/octocat/Hello-World"}`
		session := createSession(t, app, `{`+base+`,"resources":[`+publicResource+`]}`)
		defer deleteSession(t, app, session.ID)
		assertGitResourceToken(t, app, mustGitResourceRows(t, app, session.ID)[0], "")
		deployment := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, environment.ID, `"resources":[`+publicResource+`]`))
		defer cleanupDeploymentRows(t, app, deployment.ID)
		run := runDeployment(t, app, deployment.ID)
		if run.SessionID == nil {
			t.Fatal("deployment run did not create a session")
		}
		defer deleteSession(t, app, *run.SessionID)
		assertGitResourceToken(t, app, mustGitResourceRows(t, app, *run.SessionID)[0], "")
	})

	t.Run("success session token rotation with immutable repository", func(t *testing.T) {
		session := createSession(t, app, `{`+base+`,"resources":[`+resource+`]}`)
		defer deleteSession(t, app, session.ID)
		if len(session.Resources) != 1 {
			t.Fatalf("resources count = %d", len(session.Resources))
		}
		assertRawNotContains(t, session.Resources[0], "session-github-secret")
		assertRawNotContains(t, session.Resources[0], "authorization_token")
		rows := mustGitResourceRows(t, app, session.ID)
		row := rows[0]
		assertGitResourceToken(t, app, row, "session-github-secret")
		response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/resources?beta=true", strings.NewReader(resource), defaultTestKey, true)
		assertError(t, response, http.StatusBadRequest, "invalid_request_error")
		response = doSessionRequest(t, app, http.MethodDelete, "/v1/sessions/"+session.ID+"/resources/"+row.ExternalID+"?beta=true", nil, defaultTestKey, true)
		assertError(t, response, http.StatusBadRequest, "invalid_request_error")
		for _, body := range []string{`{}`, `{"authorization_token":42}`} {
			response = doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/resources/"+row.ExternalID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
			assertError(t, response, http.StatusBadRequest, "invalid_request_error")
		}
		response = doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/resources/"+row.ExternalID+"?beta=true", strings.NewReader(`{"authorization_token":"rotated-secret"}`), defaultTestKey, true)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("rotate status=%d", response.StatusCode)
		}
		var public json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&public); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		assertRawNotContains(t, public, "rotated-secret")
		rotated := mustGitResourceRows(t, app, session.ID)[0]
		assertGitResourceToken(t, app, rotated, "rotated-secret")
		if string(row.Payload) != string(rotated.Payload) {
			t.Fatal("token rotation changed repository configuration")
		}
		for _, tokenInput := range []string{`null`, `""`} {
			response = doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/resources/"+row.ExternalID+"?beta=true", strings.NewReader(`{"authorization_token":`+tokenInput+`}`), defaultTestKey, true)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("clear token status=%d", response.StatusCode)
			}
			response.Body.Close()
			assertGitResourceToken(t, app, mustGitResourceRows(t, app, session.ID)[0], "")
		}
		// Repair a pre-encryption row by rotation; plaintext is never accepted for use.
		_, err := app.db.UpdateSessionResource(context.Background(), row.WorkspaceUUID, row.SessionExternalID, row.ExternalID, row.Payload, json.RawMessage(`{"authorization_token":"legacy"}`))
		if err != nil {
			t.Fatal(err)
		}
		legacy := mustGitResourceRows(t, app, session.ID)[0]
		if _, err := sessionresource.OpenGitHubToken(context.Background(), app.vaultSecrets, gitResourceBinding(legacy), legacy.SecretPayload); err == nil {
			t.Fatal("legacy plaintext was used")
		}
		response = doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/resources/"+row.ExternalID+"?beta=true", strings.NewReader(`{"authorization_token":"repaired-secret"}`), defaultTestKey, true)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("repair status=%d", response.StatusCode)
		}
		response.Body.Close()
		assertGitResourceToken(t, app, mustGitResourceRows(t, app, session.ID)[0], "repaired-secret")
	})

	t.Run("success deployment runs rebind encrypted secrets and updates preserve them", func(t *testing.T) {
		deployment := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, environment.ID, `"resources":[`+resource+`]`))
		defer cleanupDeploymentRows(t, app, deployment.ID)
		assertRawNotContains(t, deployment.Resources, "session-github-secret")
		workspaceID := getDefaultDBIDs(t, app.pool).WorkspaceUUID
		before, err := app.db.GetDeployment(context.Background(), workspaceID, deployment.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertRawNotContains(t, before.ResourceSecrets, "session-github-secret")
		assertRawContains(t, before.ResourceSecrets, `"envelope"`)
		updateDeployment(t, app, deployment.ID, `{"name":"unchanged resources"}`)
		after, err := app.db.GetDeployment(context.Background(), workspaceID, deployment.ID)
		if err != nil {
			t.Fatal(err)
		}
		if string(after.ResourceSecrets) != string(before.ResourceSecrets) {
			t.Fatal("omitted resources changed token envelopes")
		}
		var prior *db.SessionResource
		for range 2 {
			run := runDeployment(t, app, deployment.ID)
			if run.SessionID == nil {
				t.Fatalf("deployment run failed: %s", run.Error)
			}
			defer deleteSession(t, app, *run.SessionID)
			row := mustGitResourceRows(t, app, *run.SessionID)[0]
			assertGitResourceToken(t, app, row, "session-github-secret")
			if prior != nil {
				if string(row.SecretPayload) == string(prior.SecretPayload) {
					t.Fatal("ciphertext was copied across session identities")
				}
				if _, err := sessionresource.OpenGitHubToken(context.Background(), app.vaultSecrets, gitResourceBinding(row), prior.SecretPayload); err == nil {
					t.Fatal("token opened across deployment runs")
				}
			}
			prior = &row
		}
		updateDeployment(t, app, deployment.ID, `{"resources":[`+strings.ReplaceAll(resource, "session-github-secret", "deployment-new-secret")+`]}`)
		run := runDeployment(t, app, deployment.ID)
		if run.SessionID == nil {
			t.Fatalf("updated deployment run failed: %s", run.Error)
		}
		defer deleteSession(t, app, *run.SessionID)
		assertGitResourceToken(t, app, mustGitResourceRows(t, app, *run.SessionID)[0], "deployment-new-secret")
	})
}

func mustGitResourceRows(t *testing.T, app *testApp, sessionID string) []db.SessionResource {
	t.Helper()
	session := mustSessionRecord(t, app, sessionID)
	rows, err := app.db.ListSessionResources(context.Background(), session.WorkspaceUUID, sessionID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("Git resource rows=%d err=%v", len(rows), err)
	}
	return rows
}

func gitResourceBinding(row db.SessionResource) secrets.ResourceBinding {
	return secrets.ResourceBinding{OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID, OwnerKind: "session", OwnerID: row.SessionExternalID, ResourceID: row.ExternalID}
}

func assertGitResourceToken(t *testing.T, app *testApp, row db.SessionResource, expected string) {
	t.Helper()
	if expected != "" {
		assertRawNotContains(t, row.SecretPayload, expected)
		assertRawNotContains(t, row.Payload, expected)
	} else if len(row.SecretPayload) != 0 && strings.TrimSpace(string(row.SecretPayload)) != "null" {
		t.Fatalf("anonymous resource persisted credential data: %q len=%d hex=%x", string(row.SecretPayload), len(row.SecretPayload), []byte(row.SecretPayload))
	}
	token, err := sessionresource.OpenGitHubToken(context.Background(), app.vaultSecrets, gitResourceBinding(row), row.SecretPayload)
	if err != nil || token != expected {
		t.Fatalf("resource token roundtrip failed: %v", err)
	}
}

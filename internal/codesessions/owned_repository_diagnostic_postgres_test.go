//go:build integration

package codesessions

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/dsh"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"
)

func ownedRepositoryTestStore(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "owned_repository_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	store, err := db.Open(t.Context(), config.Config{Database: config.DatabaseConfig{URL: u.String()}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestOwnedRepositoryCaptureRegistrationAndMITMPostgres(t *testing.T) {
	store := ownedRepositoryTestStore(t)
	ctx := t.Context()
	org, workspace, sidUUID, env, agent, key := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	run, nonce := strings.Repeat("b", 24), strings.Repeat("c", 32)
	snapshot, _ := json.Marshal(map[string]any{"id": "agent_owned", "version": 1, "metadata": map[string]string{"run_id": run}})
	metadata, _ := json.Marshal(map[string]string{"owned_repo_capture_nonce": nonce})
	_, err := store.ListenerPool().Exec(ctx, `INSERT INTO sessions (uuid,external_id,organization_uuid,workspace_uuid,created_by_api_key_uuid,environment_uuid,environment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,metadata,status) VALUES ($1,'sesn_owned',$2,$3,$4,$5,'env_owned',$6,'agent_owned',1,$7,$8,'running')`, sidUUID, org, workspace, key, env, agent, snapshot, metadata)
	if err != nil {
		t.Fatal(err)
	}
	repoURL := "https://github.com/owned-fixture/repo"
	commit := strings.Repeat("a", 40)
	_, err = store.CreateSessionResource(ctx, db.CreateSessionResourceInput{Resource: db.SessionResource{UUID: uuid.NewV4().String(), ExternalID: "sesrsc_owned", OrganizationUUID: org, WorkspaceUUID: workspace, SessionUUID: sidUUID, SessionExternalID: "sesn_owned", ResourceType: "github_repository", GitHubRepository: &db.SessionResourceGitHubRepository{URL: repoURL, MountPath: "/repo", Checkout: &db.SessionResourceGitHubCheckout{Type: "commit", SHA: commit}}, CreatedAt: time.Now().UTC()}})
	if err != nil {
		t.Fatal(err)
	}
	code, err := store.CreateCodeSession(ctx, db.CreateCodeSessionInput{ExternalID: "cse_owned", OrganizationUUID: org, WorkspaceUUID: workspace, SessionUUID: sidUUID, SessionExternalID: "sesn_owned", EnvironmentUUID: env, EnvironmentExternalID: "env_owned", InitialWorkerEpoch: 1, WorkDir: "/home/user", Model: "local", Metadata: []byte(`{"config":{"worker_protocol":"dsr1"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	scopeFile := filepath.Join(root, "scope.json")
	scope := ownedRepoCaptureScope{RunNonce: run, RepositoryURLSHA256: ownedRepoHash(repoURL), ExpiresAt: time.Now().Add(time.Minute), Admissions: map[string]ownedRepoAdmission{nonce: {AgentID: "agent_owned", AgentVersion: 1, AgentRunID: run, MountSHA256: ownedRepoHash("/repo"), CheckoutType: "commit", CheckoutSHA: commit}}}
	raw, _ := json.Marshal(scope)
	if err := os.WriteFile(scopeFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ownedRepoCaptureEnv, scopeFile)
	if _, _, _, _, _, ok := ownedRepoCaptureIdentityDB(ctx, store, code.ExternalID, org, workspace); !ok {
		cfg, _, valid := ownedRepoLoadScope()
		sess, found, err := store.GetSession(ctx, workspace, "sesn_owned")
		resources, resErr := store.ListSessionResources(ctx, workspace, "sesn_owned")
		t.Fatalf("valid fixture not admitted: scope=%v admissions%d session=%v/%v agent=%s/%d resources=%d/%v", valid, len(cfg.Admissions), found, err, sess.AgentExternalID, sess.AgentVersion, len(resources), resErr)
	}
	for _, wrong := range []string{"organization", "workspace", "code"} {
		c, o, w := code.ExternalID, org, workspace
		switch wrong {
		case "organization":
			o = uuid.NewV4().String()
		case "workspace":
			w = uuid.NewV4().String()
		case "code":
			c = "cse_foreign"
		}
		if _, _, _, _, _, ok := ownedRepoCaptureIdentityDB(ctx, store, c, o, w); ok {
			t.Fatal("foreign identity admitted", wrong)
		}
	}
	own := db.DSHScope{CodeSessionID: code.ExternalID, CodeSessionUUID: code.UUID, SessionID: "sesn_owned", OrganizationUUID: org, WorkspaceUUID: workspace, Epoch: 1}
	candidate := []byte(`{"model":"local","cwd":"/home/user","models":{},"config":{"worker_protocol":"dsr1","runtime_skills":[],"runtime_tools":[],"tools":[],"mcp_servers":[]}}`)
	frozen, err := store.FreezeDSHContextWithReceipt(ctx, own, candidate)
	if err != nil {
		t.Fatal(err)
	}
	replay, replayErr := store.FreezeDSHContextWithReceipt(ctx, own, candidate)
	if replayErr != nil || replay.Written || !replay.StatementAt.IsZero() || !bytes.Equal(replay.Snapshot, frozen.Snapshot) {
		t.Fatal("existing snapshot falsely claimed a fresh freeze", replayErr)
	}
	if _, err := store.ListenerPool().Exec(ctx, `UPDATE code_sessions SET current_worker_epoch=2 WHERE external_id='cse_owned'`); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	h := NewHandler(config.Config{}, newTestService(t, store), nil, slog.New(slog.NewJSONHandler(&logs, nil)))
	own.Epoch = 2
	router := chi.NewRouter()
	dsh.NewControlHandler(store, dshLifecycle{handler: h}, func(*http.Request, string) (db.DSHScope, error) { return own, nil }, func(*http.Request) error { return nil }, nil).RegisterRoutes(router)
	response := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/sessions/cse_owned/register", strings.NewReader(`{}`))
	req.Header.Set("DSR-Protocol", "1")
	req.Header.Set("Authorization", "Bearer LOCAL_TOKEN_CANARY")
	router.ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatalf("actual registration failed: %d %s", response.Code, response.Body.String())
	}
	freezeScope := own
	freezeScope.Epoch = 1
	h.service.observeOwnedRepositoryFreeze(ctx, freezeScope, candidate, frozen, []string{"/repo"}, 0)
	identity := upstreamProxyIdentity{codeSessionExternalID: code.ExternalID, organizationUUID: org, workspaceUUID: workspace}
	for _, status := range []int{200, 403, 502} {
		server, client := net.Pipe()
		done := make(chan error, 1)
		transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"X-Response-Canary": []string{"LOCAL_HEADER_CANARY"}}, Body: io.NopCloser(strings.NewReader("LOCAL_BODY_CANARY")), Request: r, ContentLength: int64(len("LOCAL_BODY_CANARY"))}, nil
		})
		go func() {
			done <- h.serveUpstreamProxyMITMHTTP(server, transport, identity, "github.com:443", "github.com")
		}()
		_ = client.SetDeadline(time.Now().Add(5 * time.Second))
		request, _ := http.NewRequest("GET", "https://github.com/owned-fixture/repo/info/refs?service=git-upload-pack", nil)
		request.Close = true
		request.Header.Set("Authorization", "LOCAL_HEADER_TOKEN_CANARY")
		if err := request.Write(client); err != nil {
			t.Fatal(err)
		}
		reply, err := http.ReadResponse(bufio.NewReader(client), request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reply.Body)
		_ = reply.Body.Close()
		_ = client.Close()
		if err != nil || reply.StatusCode != status || reply.Header.Get("X-Response-Canary") != "LOCAL_HEADER_CANARY" || string(body) != "LOCAL_BODY_CANARY" {
			t.Fatal("active capture changed real MITM HTTP response", err)
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("MITM fixture failed to close")
		}
	}
	captured, err := os.ReadFile(filepath.Join(root, "native.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(captured), "CANARY") || strings.Contains(logs.String(), "CANARY") {
		t.Fatal("headers/body/credentials leaked to capture or logs")
	}
	lines := strings.Split(strings.TrimSpace(string(captured)), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected accepted freeze, actual registration and3 actual MITM responses; got%d", len(lines))
	}
	var freeze, registration map[string]json.RawMessage
	if json.Unmarshal([]byte(lines[1]), &freeze) != nil || string(freeze["worker_epoch"]) != "1" || string(freeze["owner_epoch"]) != "1" || string(freeze["observed_owner_epoch"]) != "2" {
		t.Fatal("accepted fence confused with later observed owner")
	}
	if json.Unmarshal([]byte(lines[0]), &registration) != nil || string(registration["response_status"]) != "200" || string(registration["response_write_complete"]) != "true" || string(registration["response_epoch"]) != "2" {
		t.Fatal("actual registration response was not recorded")
	}
}

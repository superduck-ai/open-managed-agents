//go:build integration

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
	"github.com/superduck-ai/open-managed-agents/internal/platformauth"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

type qoderE2EHarness struct {
	database          *db.DB
	httpSrv           *httptest.Server
	orgUUID           string
	nativeWorkspaceID string
	sessionKey        string
	csrf              string
}

func newQoderE2EHarness(t *testing.T) *qoderE2EHarness {
	t.Helper()
	database := platformAdminAPIDatabase(t)
	adminUUID := uuid.NewV4().String()
	var orgUUID string
	var nativeWorkspaceID string
	if err := database.WithPlatformAuthTx(t.Context(), func(tx db.PlatformAuthTxStore) error {
		org, err := tx.InsertOrganization(t.Context(), db.PlatformAuthOrganizationInput{Name: "Qoder E2E 组织"})
		if err != nil {
			return err
		}
		orgUUID = org.UUID
		if _, err := tx.InsertUser(t.Context(), db.PlatformAuthUserInput{
			UUID:             adminUUID,
			ExternalID:       "user_" + strings.ReplaceAll(adminUUID, "-", "")[:20],
			OrganizationUUID: orgUUID,
			Email:            "qoder-e2e@oma.local",
			Name:             "Qoder E2E Admin",
			Role:             "admin",
		}); err != nil {
			return err
		}
		nativeWorkspaceID = "wrkspc_native_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")[:12]
		_, err = tx.InsertWorkspace(t.Context(), db.PlatformAuthWorkspaceInput{
			UUID:             uuid.NewV4().String(),
			ExternalID:       nativeWorkspaceID,
			OrganizationUUID: orgUUID,
			Name:             "Default Native",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := platformadmin.New(database, nil).SetAdministrator(t.Context(), adminUUID, "Qoder E2E Admin", "E2E 验收", true); err != nil {
		t.Fatal(err)
	}

	sessionStore := platformsession.NewMemoryStore()
	sessionKey := "sk-ant-sid-e2e-" + uuid.NewV4().String()
	if err := sessionStore.Save(t.Context(), sessionKey, platformsession.Session{
		UserUUID:             adminUUID,
		UserExternalID:       "user_e2e",
		VerifiedEmail:        "qoder-e2e@oma.local",
		OrganizationUUID:     orgUUID,
		AuthenticationMethod: "email_code_v1",
		ExpiresAt:            new(time.Now().Add(time.Hour)),
	}); err != nil {
		t.Fatal(err)
	}

	keyProvider, err := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{0x42}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	secretSvc := secrets.NewService(keyProvider)
	codeCreds, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	fsCreds, err := filestore.NewTokenCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	emailAuth := platformauth.New(config.AuthConfig{
		SMTP: config.EmailSMTPConfig{
			Addr:     "localhost:1587",
			Password: "e2e-test-password",
		},
	}, database, nil, nil)

	server := NewServer(ServerDeps{
		Config:                 config.Config{Env: config.EnvironmentDev, Storage: config.StorageConfig{MaxFileBytes: 10 * 1024 * 1024}},
		DB:                     database,
		PlatformStore:          sessionStore,
		PlatformAuth:           emailAuth,
		CodeSessionCredentials: codeCreds,
		FilestoreCredentials:   fsCreds,
		VaultSecrets:           secretSvc,
	})
	httpSrv := httptest.NewServer(server)
	t.Cleanup(httpSrv.Close)

	return &qoderE2EHarness{
		database:          database,
		httpSrv:           httpSrv,
		orgUUID:           orgUUID,
		nativeWorkspaceID: nativeWorkspaceID,
		sessionKey:        sessionKey,
		csrf:              auth.PlatformCSRFToken(sessionKey),
	}
}

func (h *qoderE2EHarness) doJSON(t *testing.T, method, path string, body any, workspaceID string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, h.httpSrv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "sessionKey", Value: h.sessionKey})
	req.AddCookie(&http.Cookie{Name: "lastActiveOrg", Value: h.orgUUID})
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-CSRF-Token", h.csrf)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if workspaceID != "" {
		req.Header.Set("X-Workspace-ID", workspaceID)
	}
	resp, err := h.httpSrv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

func (h *qoderE2EHarness) uploadFile(t *testing.T, workspaceID, filename string, content []byte) (int, []byte) {
	t.Helper()
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.httpSrv.URL+"/v1/files", &mp)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", h.csrf)
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.AddCookie(&http.Cookie{Name: "sessionKey", Value: h.sessionKey})
	req.AddCookie(&http.Cookie{Name: "lastActiveOrg", Value: h.orgUUID})
	resp, err := h.httpSrv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

type qoderSSEResult struct {
	toolUses    map[string]bool
	toolResults map[string]bool
	sawUsage    bool
	sawIdle     bool
	err         error
}

func (h *qoderE2EHarness) startSSE(t *testing.T, sessID, workspaceID string, timeout time.Duration) (<-chan qoderSSEResult, func()) {
	t.Helper()
	sseCtx, sseCancel := context.WithTimeout(t.Context(), timeout)
	req, err := http.NewRequestWithContext(sseCtx, http.MethodGet, h.httpSrv.URL+"/v1/sessions/"+sessID+"/events/stream?beta=true&event_deltas[]=agent.message", nil)
	if err != nil {
		sseCancel()
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "sessionKey", Value: h.sessionKey})
	req.AddCookie(&http.Cookie{Name: "lastActiveOrg", Value: h.orgUUID})
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		sseCancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		sseCancel()
		t.Fatalf("sse status=%d", resp.StatusCode)
	}

	resCh := make(chan qoderSSEResult, 1)
	go func() {
		defer resp.Body.Close()
		res := qoderSSEResult{toolUses: map[string]bool{}, toolResults: map[string]bool{}}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		var dataLines []string
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if len(dataLines) > 0 {
					var ev map[string]any
					if json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &ev) == nil {
						et, _ := ev["type"].(string)
						switch et {
						case "agent.tool_use":
							if id, _ := ev["id"].(string); id != "" {
								res.toolUses[id] = true
							}
						case "agent.tool_result":
							if tid, _ := ev["tool_use_id"].(string); tid != "" {
								res.toolResults[tid] = true
							}
						case "session.usage":
							res.sawUsage = true
						case "session.status_idle":
							res.sawIdle = true
							resCh <- res
							return
						}
					}
				}
				dataLines = nil
				continue
			}
			if after, ok := strings.CutPrefix(line, "data:"); ok {
				dataLines = append(dataLines, strings.TrimSpace(after))
			}
		}
		res.err = scanner.Err()
		resCh <- res
	}()
	return resCh, sseCancel
}

func newMockQoderUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	eventsReady := make(chan struct{})
	var readyOnce sync.Once
	outputDetached := false
	outputDeleted := false

	now := time.Now().UTC().Format(time.RFC3339)
	const (
		remoteEnvID     = "env_qdr_mock_1"
		remoteAgentID   = "agt_qdr_mock_1"
		remoteInFileID  = "file_qdr_in_1"
		remoteOutFileID = "file_qdr_out_1"
		remoteSessID    = "ses_qdr_mock_1"
		remoteResID     = "res_qdr_out_1"
	)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mock-qoder-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"bad token"}}`))
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/cloud")
		switch {
		case r.Method == http.MethodGet && path == "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"qmodel_latest","display_name":"QModel Latest","is_enabled":true},{"id":"disabled_model","display_name":"Disabled","is_enabled":false}]}`))
		case r.Method == http.MethodPost && path == "/environments":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":"%s","name":"Qoder Mock Env","config":{"type":"cloud"},"created_at":"%s","updated_at":"%s"}`, remoteEnvID, now, now)
		case r.Method == http.MethodGet && path == "/environments/"+remoteEnvID:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":"%s","name":"Qoder Mock Env","config":{"type":"cloud"},"created_at":"%s","updated_at":"%s"}`, remoteEnvID, now, now)
		case r.Method == http.MethodPost && path == "/agents":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":"%s","name":"Qoder Mock 助手","model":"qmodel_latest","version":1,"created_at":"%s","updated_at":"%s"}`, remoteAgentID, now, now)
		case r.Method == http.MethodGet && path == "/agents":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"data":[{"id":"%s","name":"Qoder Mock 助手","model":"qmodel_latest","version":1,"created_at":"%s","updated_at":"%s"}]}`, remoteAgentID, now, now)
		case r.Method == http.MethodGet && path == "/agents/"+remoteAgentID:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":"%s","name":"Qoder Mock 助手","model":"qmodel_latest","version":1,"created_at":"%s","updated_at":"%s"}`, remoteAgentID, now, now)
		case r.Method == http.MethodPost && path == "/files":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":"%s","filename":"input.txt","mime_type":"text/plain","size_bytes":14,"downloadable":true,"created_at":"%s"}`, remoteInFileID, now)
		case r.Method == http.MethodGet && path == "/files":
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			deleted := outputDeleted
			mu.Unlock()
			if r.URL.Query().Get("scope_id") == remoteSessID && !deleted {
				_, _ = fmt.Fprintf(w, `{"data":[{"id":"%s","filename":"report.txt","mime_type":"text/plain","size_bytes":12,"downloadable":true,"scope_id":"%s","created_at":"%s"}]}`, remoteOutFileID, remoteSessID, now)
				return
			}
			_, _ = fmt.Fprintf(w, `{"data":[{"id":"%s","filename":"input.txt","mime_type":"text/plain","size_bytes":14,"downloadable":true,"created_at":"%s"}]}`, remoteInFileID, now)
		case r.Method == http.MethodGet && path == "/files/"+remoteOutFileID+"/content":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"url":"%s/oss/report.txt","filename":"report.txt","mime_type":"text/plain"}`, srv.URL)
		case r.Method == http.MethodDelete && path == "/files/"+remoteOutFileID:
			mu.Lock()
			detached := outputDetached
			if detached {
				outputDeleted = true
			}
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if !detached {
				w.WriteHeader(http.StatusConflict)
				_, _ = fmt.Fprintf(w, `{"error":{"type":"conflict_error","message":"file still referenced"},"referenced_session_ids":["%s"]}`, remoteSessID)
				return
			}
			_, _ = fmt.Fprintf(w, `{"id":"%s","type":"file_deleted"}`, remoteOutFileID)
		case r.Method == http.MethodPost && path == "/sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":"%s","title":"Qoder Mock 会话","status":"running","agent":{"id":"%s","version":1},"environment_id":"%s","created_at":"%s","updated_at":"%s"}`, remoteSessID, remoteAgentID, remoteEnvID, now, now)
		case r.Method == http.MethodGet && path == "/sessions/"+remoteSessID:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":"%s","title":"Qoder Mock 会话","status":"idle","agent":{"id":"%s","version":1},"environment_id":"%s","created_at":"%s","updated_at":"%s"}`, remoteSessID, remoteAgentID, remoteEnvID, now, now)
		case r.Method == http.MethodGet && path == "/sessions/"+remoteSessID+"/resources":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"data":[{"id":"%s","type":"file","file_id":"%s","mount_path":"/workspace/report.txt"}]}`, remoteResID, remoteOutFileID)
		case r.Method == http.MethodDelete && path == "/sessions/"+remoteSessID+"/resources/"+remoteResID:
			mu.Lock()
			outputDetached = true
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":"%s","deleted":true}`, remoteResID)
		case r.Method == http.MethodPost && path == "/sessions/"+remoteSessID+"/events":
			readyOnce.Do(func() { close(eventsReady) })
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"data":[{"id":"evt_user_1","type":"user.message","processed_at":"%s"}]}`, now)
		case r.Method == http.MethodGet && path == "/sessions/"+remoteSessID+"/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			if flusher != nil {
				flusher.Flush()
			}
			select {
			case <-eventsReady:
			case <-r.Context().Done():
				return
			}
			frames := []struct {
				id   string
				data string
			}{
				{id: "evt_tu_1", data: fmt.Sprintf(`{"id":"evt_tu_1","type":"agent.tool_use","name":"Write","input":{"path":"report.txt"},"processed_at":"%s"}`, now)},
				{id: "evt_tr_1", data: fmt.Sprintf(`{"id":"evt_tr_1","type":"agent.tool_result","tool_use_id":"evt_tu_1","content":[{"type":"text","text":"ok"}],"is_error":false,"processed_at":"%s"}`, now)},
				{id: "evt_usg_1", data: fmt.Sprintf(`{"id":"evt_usg_1","type":"session.usage","usage":{"model_credits":0.42,"sandbox_runtime_credits":0.08,"total_credits":0.50},"processed_at":"%s"}`, now)},
				{id: "evt_idle_1", data: fmt.Sprintf(`{"id":"evt_idle_1","type":"session.status_idle","stop_reason":{"type":"end_turn"},"processed_at":"%s"}`, now)},
			}
			for _, f := range frames {
				_, _ = fmt.Fprintf(w, "id: %s\ndata: %s\n\n", f.id, f.data)
				if flusher != nil {
					flusher.Flush()
				}
			}
			<-r.Context().Done()
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":{"type":"not_found_error","message":"unhandled %s %s"}}`, r.Method, path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestQoderSharedPoolEndToEndMockPostgres(t *testing.T) {
	h := newQoderE2EHarness(t)
	mockQoder := newMockQoderUpstream(t)

	st, raw := h.doJSON(t, http.MethodPost, "/api/platform-admin/runtime-profiles", map[string]any{
		"name":           "Qoder 共享池 Mock E2E",
		"provider_kind":  "qoder_cloud",
		"endpoint_url":   mockQoder.URL + "/api/v1/cloud",
		"isolation_mode": "shared_upstream",
		"credentials":    map[string]string{"token": "mock-qoder-token"},
		"status":         "active",
	}, "")
	if st != http.StatusCreated {
		t.Fatalf("create runtime profile status=%d body=%s", st, raw)
	}
	var profile struct {
		UUID string `json:"uuid"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}

	st, raw = h.doJSON(t, http.MethodPost, "/api/platform-admin/runtime-profiles/"+profile.ID+"/test", nil, "")
	if st != http.StatusOK {
		t.Fatalf("health check status=%d body=%s", st, raw)
	}

	st, raw = h.doJSON(t, http.MethodPut, "/api/platform-admin/organizations/"+h.orgUUID+"/runtime-policy", map[string]any{
		"default_runtime_profile_uuid":  db.DefaultOMANativeRuntimeProfileUUID,
		"allowed_runtime_profile_uuids": []string{db.DefaultOMANativeRuntimeProfileUUID, profile.UUID},
	}, "")
	if st != http.StatusOK {
		t.Fatalf("update org runtime policy status=%d body=%s", st, raw)
	}

	createWS := func(name string) string {
		st, raw := h.doJSON(t, http.MethodPost, "/api/console/organizations/"+h.orgUUID+"/workspaces", map[string]any{
			"name":               name,
			"runtime_profile_id": profile.ID,
		}, "")
		if st != http.StatusCreated && st != http.StatusOK {
			t.Fatalf("create workspace %s status=%d body=%s", name, st, raw)
		}
		var ws struct {
			ID      string `json:"id"`
			Runtime struct {
				ProviderKind string `json:"provider_kind"`
				Capabilities struct {
					MCPTunnels      bool   `json:"mcp_tunnels"`
					Deployments     bool   `json:"deployments"`
					SessionWebhooks bool   `json:"session_webhooks"`
					SessionBudget   string `json:"session_budget"`
				} `json:"capabilities"`
			} `json:"runtime"`
		}
		if err := json.Unmarshal(raw, &ws); err != nil {
			t.Fatal(err)
		}
		if ws.Runtime.ProviderKind != "qoder_cloud" ||
			ws.Runtime.Capabilities.MCPTunnels ||
			!ws.Runtime.Capabilities.Deployments ||
			ws.Runtime.Capabilities.SessionWebhooks ||
			ws.Runtime.Capabilities.SessionBudget != "credits" {
			t.Fatalf("unexpected workspace runtime: %+v", ws.Runtime)
		}
		return ws.ID
	}

	wsQ1 := createWS("Qoder Mock 空间 1")
	wsQ2 := createWS("Qoder Mock 空间 2")

	st, raw = h.doJSON(t, http.MethodGet, "/api/console/organizations/"+h.orgUUID+"/workspaces/"+wsQ1+"/mcp_tunnels", nil, h.nativeWorkspaceID)
	if st != http.StatusBadRequest || !strings.Contains(string(raw), "unsupported_capability_error") {
		t.Fatalf("expected MCP tunnel cross-workspace guard 400, got status=%d body=%s", st, raw)
	}
	st, raw = h.doJSON(t, http.MethodGet, "/v1/deployments?beta=true", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("expected deployments enabled on qoder 200, got status=%d body=%s", st, raw)
	}

	st, raw = h.doJSON(t, http.MethodGet, "/v1/models", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("list models status=%d body=%s", st, raw)
	}
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &modelsResp); err != nil || len(modelsResp.Data) != 1 || modelsResp.Data[0].ID != "qmodel_latest" {
		t.Fatalf("expected only enabled model qmodel_latest, got %s", raw)
	}

	var idObj struct {
		ID string `json:"id"`
	}
	st, raw = h.doJSON(t, http.MethodPost, "/v1/environments?beta=true", map[string]any{
		"name":   "Qoder Mock Env",
		"config": map[string]any{"type": "cloud"},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create env status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	envID := idObj.ID

	st, raw = h.doJSON(t, http.MethodPost, "/v1/agents?beta=true", map[string]any{
		"name":   "Qoder Mock 助手",
		"model":  "qmodel_latest",
		"system": "mock",
		"tools": []map[string]any{{
			"type":           "agent_toolset_20260401",
			"default_config": map[string]any{"enabled": true},
			"configs":        []map[string]any{{"name": "bash", "enabled": true}},
		}},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create agent status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	agentID := idObj.ID

	st, raw = h.doJSON(t, http.MethodPost, "/v1/agents:search?beta=true", map[string]any{"name": "Mock"}, wsQ1)
	if st != http.StatusOK || !strings.Contains(string(raw), agentID) {
		t.Fatalf("agents:search status=%d body=%s", st, raw)
	}

	st, raw = h.uploadFile(t, wsQ1, "input.txt", []byte("hello oma e2e\n"))
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("upload file status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	inputFileID := idObj.ID

	st, raw = h.doJSON(t, http.MethodPost, "/v1/sessions?beta=true", map[string]any{
		"agent":          agentID,
		"environment_id": envID,
		"title":          "Qoder Mock 会话",
		"resources":      []map[string]any{{"type": "file", "file_id": inputFileID, "mount_path": "/input.txt"}},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create session status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	sessID := idObj.ID
	if !strings.HasPrefix(sessID, "sesn_") {
		t.Fatalf("expected sesn_ prefix, got %s", sessID)
	}

	for _, check := range []string{"/v1/sessions/" + sessID + "?beta=true", "/v1/agents/" + agentID + "?beta=true", "/v1/files/" + inputFileID} {
		stIDOR, rawIDOR := h.doJSON(t, http.MethodGet, check, nil, wsQ2)
		if stIDOR != http.StatusNotFound {
			t.Fatalf("IDOR breach on %s: status=%d body=%s", check, stIDOR, rawIDOR)
		}
	}

	resCh, sseCancel := h.startSSE(t, sessID, wsQ1, 15*time.Second)
	defer sseCancel()

	st, raw = h.doJSON(t, http.MethodPost, "/v1/sessions/"+sessID+"/events?beta=true", map[string]any{
		"events": []map[string]any{{
			"type":    "user.message",
			"content": []map[string]any{{"type": "text", "text": "run mock"}},
		}},
	}, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("send event status=%d body=%s", st, raw)
	}

	res := <-resCh
	if !res.sawIdle || !res.sawUsage {
		t.Fatalf("expected idle and usage events, got %+v", res)
	}
	if !res.toolUses["sevt_qdr_tu_1"] || !res.toolResults["sevt_qdr_tu_1"] {
		t.Fatalf("expected encoded tool_use/tool_result pairing sevt_qdr_tu_1, uses=%v results=%v", res.toolUses, res.toolResults)
	}

	var usageCount int
	var totalCredits float64
	if err := h.database.SQLDB().QueryRowContext(t.Context(), `SELECT COUNT(*), COALESCE(SUM(delta_amount), 0) FROM provider_usage_records WHERE session_external_id = $1 AND provider_kind = 'qoder_cloud' AND metric_unit = 'credits'`, sessID).Scan(&usageCount, &totalCredits); err != nil {
		t.Fatal(err)
	}
	if usageCount < 1 || totalCredits < 0.49 {
		t.Fatalf("expected provider_usage_records persisted for %s, got count=%d credits=%v", sessID, usageCount, totalCredits)
	}

	st, raw = h.doJSON(t, http.MethodGet, "/v1/files?scope_id="+sessID, nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("list scoped files status=%d body=%s", st, raw)
	}
	var filesResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &filesResp); err != nil || len(filesResp.Data) != 1 {
		t.Fatalf("expected 1 claimed output file, got %s", raw)
	}
	outFileID := filesResp.Data[0].ID

	st, raw = h.doJSON(t, http.MethodGet, "/v1/files/"+outFileID+"/content", nil, wsQ1)
	if st == http.StatusOK {
		t.Fatalf("expected SSRF block for loopback download URL, got 200 body=%s", raw)
	}

	st, raw = h.doJSON(t, http.MethodGet, "/api/organizations/"+h.orgUUID+"/workspaces/"+wsQ1+"/overview?days=7", nil, "")
	if st != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", st, raw)
	}
	var overview struct {
		AgentCount   int64 `json:"agent_count"`
		SessionCount int64 `json:"session_count"`
	}
	if err := json.Unmarshal(raw, &overview); err != nil || overview.AgentCount < 1 || overview.SessionCount < 1 {
		t.Fatalf("unexpected overview: %+v raw=%s", overview, raw)
	}

	st, raw = h.doJSON(t, http.MethodDelete, "/v1/files/"+outFileID, nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("output file 409 two-step delete failed: status=%d body=%s", st, raw)
	}

	st, raw = h.doJSON(t, http.MethodGet, "/v1/sessions?beta=true", nil, h.nativeWorkspaceID)
	if st != http.StatusOK {
		t.Fatalf("native workspace zero regression check failed: status=%d body=%s", st, raw)
	}
}

func TestQoderSharedPoolEndToEndPostgres(t *testing.T) {
	liveToken := strings.TrimSpace(os.Getenv("QODER_LIVE_TOKEN"))
	if liveToken == "" {
		t.Skip("set QODER_LIVE_TOKEN to run full live Qoder E2E against PostgreSQL")
	}
	h := newQoderE2EHarness(t)

	st, raw := h.doJSON(t, http.MethodPost, "/api/platform-admin/runtime-profiles", map[string]any{
		"name":           "Qoder 共享池 Live E2E",
		"provider_kind":  "qoder_cloud",
		"endpoint_url":   "https://api.qoder.com.cn/api/v1/cloud",
		"isolation_mode": "shared_upstream",
		"credentials":    map[string]string{"token": liveToken},
		"status":         "active",
	}, "")
	if st != http.StatusCreated {
		t.Fatalf("create runtime profile status=%d body=%s", st, raw)
	}
	var profile struct {
		UUID string `json:"uuid"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}

	st, raw = h.doJSON(t, http.MethodPost, "/api/platform-admin/runtime-profiles/"+profile.ID+"/test", nil, "")
	if st != http.StatusOK {
		t.Fatalf("health check status=%d body=%s", st, raw)
	}

	st, raw = h.doJSON(t, http.MethodPut, "/api/platform-admin/organizations/"+h.orgUUID+"/runtime-policy", map[string]any{
		"default_runtime_profile_uuid":  db.DefaultOMANativeRuntimeProfileUUID,
		"allowed_runtime_profile_uuids": []string{db.DefaultOMANativeRuntimeProfileUUID, profile.UUID},
	}, "")
	if st != http.StatusOK {
		t.Fatalf("update org runtime policy status=%d body=%s", st, raw)
	}

	createWS := func(name string) string {
		st, raw := h.doJSON(t, http.MethodPost, "/api/console/organizations/"+h.orgUUID+"/workspaces", map[string]any{
			"name":               name,
			"runtime_profile_id": profile.ID,
		}, "")
		if st != http.StatusCreated && st != http.StatusOK {
			t.Fatalf("create workspace %s status=%d body=%s", name, st, raw)
		}
		var ws struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &ws); err != nil {
			t.Fatal(err)
		}
		return ws.ID
	}

	wsQ1 := createWS("Qoder Live 空间 1")
	wsQ2 := createWS("Qoder Live 空间 2")

	st, raw = h.doJSON(t, http.MethodGet, "/v1/models", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("list models status=%d body=%s", st, raw)
	}
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &modelsResp); err != nil || len(modelsResp.Data) == 0 {
		t.Fatalf("models decode=%v body=%s", err, raw)
	}
	modelID := modelsResp.Data[0].ID

	var envID, agentID, inputFileID, sessID, outFileID string
	t.Cleanup(func() {
		cleanup := func(method, path string) {
			req, _ := http.NewRequestWithContext(context.Background(), method, h.httpSrv.URL+path, nil)
			req.AddCookie(&http.Cookie{Name: "sessionKey", Value: h.sessionKey})
			req.AddCookie(&http.Cookie{Name: "lastActiveOrg", Value: h.orgUUID})
			req.Header.Set("X-CSRF-Token", h.csrf)
			req.Header.Set("X-Workspace-ID", wsQ1)
			resp, err := h.httpSrv.Client().Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
		}
		if sessID != "" {
			cleanup(http.MethodPost, "/v1/sessions/"+sessID+"/archive?beta=true")
			cleanup(http.MethodDelete, "/v1/sessions/"+sessID+"?beta=true")
		}
		if outFileID != "" {
			cleanup(http.MethodDelete, "/v1/files/"+outFileID)
		}
		if inputFileID != "" {
			cleanup(http.MethodDelete, "/v1/files/"+inputFileID)
		}
		if agentID != "" {
			cleanup(http.MethodPost, "/v1/agents/"+agentID+"/archive?beta=true")
		}
		if envID != "" {
			cleanup(http.MethodDelete, "/v1/environments/"+envID+"?beta=true")
		}
	})

	var idObj struct {
		ID string `json:"id"`
	}
	st, raw = h.doJSON(t, http.MethodPost, "/v1/environments?beta=true", map[string]any{
		"name":   "Qoder Live Env",
		"config": map[string]any{"type": "cloud"},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create env status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	envID = idObj.ID

	st, raw = h.doJSON(t, http.MethodPost, "/v1/agents?beta=true", map[string]any{
		"name":   "Qoder Live 助手",
		"model":  modelID,
		"system": "Use Write to create report.txt containing exactly OMA_E2E_PASS, then call DeliverArtifacts on report.txt, then reply DONE.",
		"tools": []map[string]any{{
			"type":           "agent_toolset_20260401",
			"default_config": map[string]any{"enabled": true},
			"configs":        []map[string]any{{"name": "bash", "enabled": true}, {"name": "write", "enabled": true}},
		}},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create agent status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	agentID = idObj.ID

	st, raw = h.uploadFile(t, wsQ1, "input.txt", []byte("hello oma live\n"))
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("upload file status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	inputFileID = idObj.ID

	st, raw = h.doJSON(t, http.MethodPost, "/v1/sessions?beta=true", map[string]any{
		"agent":          agentID,
		"environment_id": envID,
		"title":          "Qoder Live 会话",
		"resources":      []map[string]any{{"type": "file", "file_id": inputFileID, "mount_path": "/input.txt"}},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create session status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	sessID = idObj.ID

	stIDOR, rawIDOR := h.doJSON(t, http.MethodGet, "/v1/sessions/"+sessID+"?beta=true", nil, wsQ2)
	if stIDOR != http.StatusNotFound {
		t.Fatalf("IDOR breach on live session: status=%d body=%s", stIDOR, rawIDOR)
	}

	resCh, sseCancel := h.startSSE(t, sessID, wsQ1, 110*time.Second)
	defer sseCancel()

	st, raw = h.doJSON(t, http.MethodPost, "/v1/sessions/"+sessID+"/events?beta=true", map[string]any{
		"events": []map[string]any{{
			"type":    "user.message",
			"content": []map[string]any{{"type": "text", "text": "Write report.txt with exact text OMA_E2E_PASS, then call DeliverArtifacts on report.txt, then reply DONE."}},
		}},
	}, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("send event status=%d body=%s", st, raw)
	}

	res := <-resCh
	if !res.sawIdle || !res.sawUsage || len(res.toolUses) == 0 || len(res.toolResults) == 0 {
		t.Fatalf("live SSE assertion failed: %+v", res)
	}
	for tid := range res.toolResults {
		if !strings.HasPrefix(tid, "sevt_qdr_") || !res.toolUses[tid] {
			t.Fatalf("live tool_use/tool_result pairing failed: tid=%s uses=%v", tid, res.toolUses)
		}
	}

	st, raw = h.doJSON(t, http.MethodGet, "/v1/files?scope_id="+sessID, nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("list scoped files status=%d body=%s", st, raw)
	}
	var filesResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &filesResp); err != nil || len(filesResp.Data) == 0 {
		t.Fatalf("expected output files, got %s", raw)
	}
	outFileID = filesResp.Data[0].ID

	st, raw = h.doJSON(t, http.MethodGet, "/v1/files/"+outFileID+"/content", nil, wsQ1)
	if st != http.StatusOK || strings.TrimSpace(string(raw)) != "OMA_E2E_PASS" {
		t.Fatalf("proxy file download failed: status=%d body=%q", st, string(raw))
	}

	st, raw = h.doJSON(t, http.MethodGet, "/api/console/organizations/"+h.orgUUID+"/workspaces/"+wsQ1+"/files/"+outFileID+"/content", nil, "")
	if st != http.StatusOK || strings.TrimSpace(string(raw)) != "OMA_E2E_PASS" {
		t.Fatalf("console file preview failed: status=%d body=%q", st, string(raw))
	}

	st, raw = h.doJSON(t, http.MethodDelete, "/v1/files/"+outFileID, nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("output file 409 two-step delete failed: status=%d body=%s", st, raw)
	}
	outFileID = ""
}

func (h *qoderE2EHarness) uploadSkill(t *testing.T, workspaceID, path, displayTitle, skillName, skillBody string) (int, []byte) {
	t.Helper()
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	if displayTitle != "" {
		if err := mw.WriteField("display_title", displayTitle); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("files[]", skillName+"/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(skillBody)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.httpSrv.URL+path, &mp)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("anthropic-beta", "skills-2025-10-02")
	req.Header.Set("X-CSRF-Token", h.csrf)
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.AddCookie(&http.Cookie{Name: "sessionKey", Value: h.sessionKey})
	req.AddCookie(&http.Cookie{Name: "lastActiveOrg", Value: h.orgUUID})
	resp, err := h.httpSrv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

func TestQoderBatchResourcesEndToEndPostgres(t *testing.T) {
	liveToken := strings.TrimSpace(os.Getenv("QODER_LIVE_TOKEN"))
	if liveToken == "" {
		t.Skip("set QODER_LIVE_TOKEN to run batch 1-4 live Qoder E2E against PostgreSQL")
	}
	h := newQoderE2EHarness(t)

	st, raw := h.doJSON(t, http.MethodPost, "/api/platform-admin/runtime-profiles", map[string]any{
		"name":           "Qoder Batch Live E2E",
		"provider_kind":  "qoder_cloud",
		"endpoint_url":   "https://api.qoder.com.cn/api/v1/cloud",
		"isolation_mode": "shared_upstream",
		"credentials":    map[string]string{"token": liveToken},
		"status":         "active",
	}, "")
	if st != http.StatusCreated {
		t.Fatalf("create runtime profile status=%d body=%s", st, raw)
	}
	var profile struct {
		UUID string `json:"uuid"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}

	st, raw = h.doJSON(t, http.MethodPut, "/api/platform-admin/organizations/"+h.orgUUID+"/runtime-policy", map[string]any{
		"default_runtime_profile_uuid":  db.DefaultOMANativeRuntimeProfileUUID,
		"allowed_runtime_profile_uuids": []string{db.DefaultOMANativeRuntimeProfileUUID, profile.UUID},
	}, "")
	if st != http.StatusOK {
		t.Fatalf("update org runtime policy status=%d body=%s", st, raw)
	}

	createWS := func(name string) string {
		st, raw := h.doJSON(t, http.MethodPost, "/api/console/organizations/"+h.orgUUID+"/workspaces", map[string]any{
			"name":               name,
			"runtime_profile_id": profile.ID,
		}, "")
		if st != http.StatusCreated && st != http.StatusOK {
			t.Fatalf("create workspace %s status=%d body=%s", name, st, raw)
		}
		var ws struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &ws); err != nil {
			t.Fatal(err)
		}
		return ws.ID
	}

	wsQ1 := createWS("Qoder Batch 1-4 空间 A")
	wsQ2 := createWS("Qoder Batch 1-4 空间 B")

	st, raw = h.doJSON(t, http.MethodGet, "/v1/models", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("list models status=%d body=%s", st, raw)
	}
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &modelsResp); err != nil || len(modelsResp.Data) == 0 {
		t.Fatalf("models decode=%v body=%s", err, raw)
	}
	modelID := modelsResp.Data[0].ID

	var (
		storeID      string
		envID        string
		agentID      string
		skillAgentID string
		depID        string
		runSessionID string
		vaultSessID  string
		skillID      string
		skillVer     string
		vaultID      string
		credID       string
	)
	t.Cleanup(func() {
		cleanup := func(method, path string) {
			req, _ := http.NewRequestWithContext(context.Background(), method, h.httpSrv.URL+path, nil)
			req.AddCookie(&http.Cookie{Name: "sessionKey", Value: h.sessionKey})
			req.AddCookie(&http.Cookie{Name: "lastActiveOrg", Value: h.orgUUID})
			req.Header.Set("X-CSRF-Token", h.csrf)
			req.Header.Set("X-Workspace-ID", wsQ1)
			resp, err := h.httpSrv.Client().Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
		}
		for _, sid := range []string{runSessionID, vaultSessID} {
			if sid != "" {
				cleanup(http.MethodPost, "/v1/sessions/"+sid+"/archive?beta=true")
				cleanup(http.MethodDelete, "/v1/sessions/"+sid+"?beta=true")
			}
		}
		if depID != "" {
			cleanup(http.MethodPost, "/v1/deployments/"+depID+"/archive?beta=true")
		}
		for _, aid := range []string{skillAgentID, agentID} {
			if aid != "" {
				cleanup(http.MethodPost, "/v1/agents/"+aid+"/archive?beta=true")
			}
		}
		if skillID != "" {
			if skillVer != "" {
				cleanup(http.MethodDelete, "/v1/skills/"+skillID+"/versions/"+skillVer)
			}
			cleanup(http.MethodDelete, "/v1/skills/"+skillID)
		}
		if vaultID != "" {
			if credID != "" {
				cleanup(http.MethodDelete, "/v1/vaults/"+vaultID+"/credentials/"+credID)
			}
			cleanup(http.MethodDelete, "/v1/vaults/"+vaultID)
		}
		if storeID != "" {
			cleanup(http.MethodPost, "/v1/memory_stores/"+storeID+"/archive")
			cleanup(http.MethodDelete, "/v1/memory_stores/"+storeID)
		}
		if envID != "" {
			cleanup(http.MethodDelete, "/v1/environments/"+envID+"?beta=true")
		}
	})

	var idObj struct {
		ID string `json:"id"`
	}

	st, raw = h.doJSON(t, http.MethodPost, "/v1/memory_stores", map[string]any{
		"name":        "OMA E2E Memory Store",
		"description": "Batch 1 live check",
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create memory store status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	storeID = idObj.ID

	if stIDOR, rawIDOR := h.doJSON(t, http.MethodGet, "/v1/memory_stores/"+storeID, nil, wsQ2); stIDOR != http.StatusNotFound {
		t.Fatalf("memory store IDOR breach status=%d body=%s", stIDOR, rawIDOR)
	}

	st, raw = h.doJSON(t, http.MethodPost, "/v1/memory_stores/"+storeID+"/memories", map[string]any{
		"path":    "/notes/e2e.md",
		"content": "OMA_MEMORY_LIVE_OK",
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create memory status=%d body=%s", st, raw)
	}
	var memResp struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &memResp)
	if memResp.ID == "" || memResp.Path != "/notes/e2e.md" {
		t.Fatalf("unexpected memory response: %s", raw)
	}

	st, raw = h.doJSON(t, http.MethodGet, "/v1/memory_stores/"+storeID+"/memories?view=full", nil, wsQ1)
	if st != http.StatusOK || !strings.Contains(string(raw), "OMA_MEMORY_LIVE_OK") {
		t.Fatalf("list memories view=full status=%d body=%s", st, raw)
	}

	st, raw = h.doJSON(t, http.MethodGet, "/v1/memory_stores/"+storeID+"/memory_versions", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("list memory versions status=%d body=%s", st, raw)
	}
	var versResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &versResp); err != nil || len(versResp.Data) == 0 {
		t.Fatalf("memory versions decode=%v body=%s", err, raw)
	}
	st, raw = h.doJSON(t, http.MethodPost, "/v1/memory_stores/"+storeID+"/memory_versions/"+versResp.Data[0].ID+"/redact", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("redact memory version status=%d body=%s", st, raw)
	}

	st, raw = h.doJSON(t, http.MethodPost, "/v1/environments?beta=true", map[string]any{
		"name":   "OMA Batch Live Env",
		"config": map[string]any{"type": "cloud"},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create env status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	envID = idObj.ID

	st, raw = h.doJSON(t, http.MethodPost, "/v1/agents?beta=true", map[string]any{
		"name":   "OMA Batch Live Agent",
		"model":  modelID,
		"system": "Reply OK immediately.",
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create agent status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	agentID = idObj.ID

	st, raw = h.doJSON(t, http.MethodPost, "/v1/deployments?beta=true", map[string]any{
		"agent":          agentID,
		"environment_id": envID,
		"name":           "OMA Batch Live Deployment",
		"initial_events": []map[string]any{{
			"type":    "user.message",
			"content": []map[string]any{{"type": "text", "text": "Reply OK"}},
		}},
		"resources": []map[string]any{{
			"type":            "memory_store",
			"memory_store_id": storeID,
			"access":          "read_write",
		}},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create deployment status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	depID = idObj.ID

	if stIDOR, rawIDOR := h.doJSON(t, http.MethodGet, "/v1/deployments/"+depID+"?beta=true", nil, wsQ2); stIDOR != http.StatusNotFound {
		t.Fatalf("deployment IDOR breach status=%d body=%s", stIDOR, rawIDOR)
	}

	st, raw = h.doJSON(t, http.MethodPost, "/v1/deployments/"+depID+"?beta=true", map[string]any{
		"name": "OMA Batch Live Deployment Updated",
	}, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("update deployment status=%d body=%s", st, raw)
	}
	st, raw = h.doJSON(t, http.MethodPost, "/v1/deployments/"+depID+"/pause?beta=true", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("pause deployment status=%d body=%s", st, raw)
	}
	st, raw = h.doJSON(t, http.MethodPost, "/v1/deployments/"+depID+"/unpause?beta=true", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("unpause deployment status=%d body=%s", st, raw)
	}

	st, raw = h.doJSON(t, http.MethodPost, "/v1/deployments/"+depID+"/run?beta=true", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("run deployment status=%d body=%s", st, raw)
	}
	var runResp struct {
		ID           string  `json:"id"`
		DeploymentID string  `json:"deployment_id"`
		SessionID    *string `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &runResp); err != nil || runResp.ID == "" || runResp.DeploymentID != depID || runResp.SessionID == nil || *runResp.SessionID == "" {
		t.Fatalf("unexpected deployment run response: %s", raw)
	}
	runSessionID = *runResp.SessionID

	if stSess, rawSess := h.doJSON(t, http.MethodGet, "/v1/sessions/"+runSessionID+"?beta=true", nil, wsQ1); stSess != http.StatusOK {
		t.Fatalf("claimed deployment run session inaccessible in owner workspace: status=%d body=%s", stSess, rawSess)
	}
	if stIDOR, rawIDOR := h.doJSON(t, http.MethodGet, "/v1/sessions/"+runSessionID+"?beta=true", nil, wsQ2); stIDOR != http.StatusNotFound {
		t.Fatalf("claimed deployment run session IDOR breach: status=%d body=%s", stIDOR, rawIDOR)
	}
	if stRuns, rawRuns := h.doJSON(t, http.MethodGet, "/v1/deployment_runs?beta=true&deployment_id="+depID, nil, wsQ1); stRuns != http.StatusOK || !strings.Contains(string(rawRuns), runResp.ID) {
		t.Fatalf("list deployment runs status=%d body=%s", stRuns, rawRuns)
	}

	skillSlug := "oma-live-" + strings.ToLower(strings.ReplaceAll(uuid.NewV4().String(), "-", "")[:8])
	st, raw = h.uploadSkill(t, wsQ1, "/v1/skills", "OMA Live Skill "+skillSlug, skillSlug, "---\nname: "+skillSlug+"\ndescription: batch 3 live skill\n---\n# Live Skill\n")
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create skill status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	skillID = idObj.ID

	if stIDOR, rawIDOR := h.doJSON(t, http.MethodGet, "/v1/skills/"+skillID, nil, wsQ2); stIDOR != http.StatusNotFound {
		t.Fatalf("skill IDOR breach status=%d body=%s", stIDOR, rawIDOR)
	}

	st, raw = h.uploadSkill(t, wsQ1, "/v1/skills/"+skillID+"/versions", "", skillSlug, "---\nname: "+skillSlug+"\ndescription: batch 3 live skill v2\n---\n# Live Skill v2\n")
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create skill version status=%d body=%s", st, raw)
	}
	var skVerResp struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(raw, &skVerResp)
	skillVer = skVerResp.Version

	st, raw = h.doJSON(t, http.MethodGet, "/v1/skills/"+skillID+"/versions", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("list skill versions status=%d body=%s", st, raw)
	}
	var skVersList struct {
		Data []struct {
			ID      string `json:"id"`
			SkillID string `json:"skill_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &skVersList); err != nil || len(skVersList.Data) < 2 || skVersList.Data[0].SkillID != skillID {
		t.Fatalf("list skill versions unexpected body=%s", raw)
	}

	st, raw = h.doJSON(t, http.MethodPost, "/v1/agents?beta=true", map[string]any{
		"name":  "OMA Skilled Agent",
		"model": modelID,
		"skills": []map[string]any{{
			"type":     "custom",
			"skill_id": skillID,
			"version":  "latest",
		}},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create agent with skill status=%d body=%s", st, raw)
	}
	if !strings.Contains(string(raw), skillID) {
		t.Fatalf("expected agent skills mapped back to oma skill_id %q: %s", skillID, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	skillAgentID = idObj.ID

	st, raw = h.doJSON(t, http.MethodPost, "/v1/vaults", map[string]any{
		"display_name": "OMA Batch Live Vault",
		"metadata":     map[string]string{"env": "e2e"},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create vault status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	vaultID = idObj.ID

	if stIDOR, rawIDOR := h.doJSON(t, http.MethodGet, "/v1/vaults/"+vaultID, nil, wsQ2); stIDOR != http.StatusNotFound {
		t.Fatalf("vault IDOR breach status=%d body=%s", stIDOR, rawIDOR)
	}
	if stUpd, rawUpd := h.doJSON(t, http.MethodPost, "/v1/vaults/"+vaultID, map[string]any{"display_name": "Should 501"}, wsQ1); stUpd != http.StatusNotImplemented || !strings.Contains(string(rawUpd), "feature_not_available") {
		t.Fatalf("update vault status=%d body=%s, want 501 feature_not_available", stUpd, rawUpd)
	}

	st, raw = h.doJSON(t, http.MethodPost, "/v1/vaults/"+vaultID+"/credentials", map[string]any{
		"display_name": "E2E Bearer",
		"metadata":     map[string]string{"owner": "e2e"},
		"auth": map[string]any{
			"type":           "static_bearer",
			"mcp_server_url": "https://mcp.example.com",
			"token":          "tok_e2e_dummy",
		},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create credential status=%d body=%s", st, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	credID = idObj.ID

	st, raw = h.doJSON(t, http.MethodPost, "/v1/vaults/"+vaultID+"/credentials/"+credID, map[string]any{
		"display_name": "Ignored On Qoder",
		"metadata":     map[string]string{"owner": "e2e-v2"},
	}, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("update credential status=%d body=%s", st, raw)
	}

	st, raw = h.doJSON(t, http.MethodPost, "/v1/sessions?beta=true", map[string]any{
		"agent":          agentID,
		"environment_id": envID,
		"title":          "OMA Vault Session",
		"vault_ids":      []string{vaultID},
	}, wsQ1)
	if st != http.StatusOK && st != http.StatusCreated {
		t.Fatalf("create session with vault status=%d body=%s", st, raw)
	}
	if !strings.Contains(string(raw), vaultID) {
		t.Fatalf("expected session vault_ids mapped back to oma vault_id %q: %s", vaultID, raw)
	}
	_ = json.Unmarshal(raw, &idObj)
	vaultSessID = idObj.ID

	st, raw = h.doJSON(t, http.MethodPost, "/v1/vaults/"+vaultID+"/credentials/"+credID+"/archive", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("archive credential status=%d body=%s", st, raw)
	}
	st, raw = h.doJSON(t, http.MethodDelete, "/v1/vaults/"+vaultID+"/credentials/"+credID, nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("delete credential status=%d body=%s", st, raw)
	}
	credID = ""
	st, raw = h.doJSON(t, http.MethodPost, "/v1/vaults/"+vaultID+"/archive", nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("archive vault status=%d body=%s", st, raw)
	}
	st, raw = h.doJSON(t, http.MethodDelete, "/v1/vaults/"+vaultID, nil, wsQ1)
	if st != http.StatusOK {
		t.Fatalf("delete vault status=%d body=%s", st, raw)
	}
	vaultID = ""
}

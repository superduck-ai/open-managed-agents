package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type testBindingStore struct {
	mu       sync.Mutex
	byOMA    map[string]db.RuntimeResourceBinding
	byRemote map[string]db.RuntimeResourceBinding
	order    []string
}

func newTestBindingStore() *testBindingStore {
	return &testBindingStore{
		byOMA:    make(map[string]db.RuntimeResourceBinding),
		byRemote: make(map[string]db.RuntimeResourceBinding),
	}
}

func makeOMAKey(org, ws, rt, id string) string    { return org + "|" + ws + "|" + rt + "|" + id }
func makeRemoteKey(profile, rt, id string) string { return profile + "|" + rt + "|" + id }

func (s *testBindingStore) UpsertRuntimeResourceBinding(_ context.Context, b db.RuntimeResourceBinding) (db.RuntimeResourceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	b.UpdatedAt = now
	k := makeOMAKey(b.OrganizationUUID, b.WorkspaceUUID, b.ResourceType, b.OMAExternalID)
	if _, exists := s.byOMA[k]; !exists {
		s.order = append(s.order, k)
	}
	s.byOMA[k] = b
	s.byRemote[makeRemoteKey(b.RuntimeProfileUUID, b.ResourceType, b.RemoteID)] = b
	return b, nil
}

func (s *testBindingStore) InsertRuntimeResourceBindingIfAbsent(_ context.Context, b db.RuntimeResourceBinding) (db.RuntimeResourceBinding, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rk := makeRemoteKey(b.RuntimeProfileUUID, b.ResourceType, b.RemoteID)
	if existing, exists := s.byRemote[rk]; exists && existing.State != "deleted" {
		return db.RuntimeResourceBinding{}, false, nil
	}
	k := makeOMAKey(b.OrganizationUUID, b.WorkspaceUUID, b.ResourceType, b.OMAExternalID)
	if existing, exists := s.byOMA[k]; exists && existing.State != "deleted" {
		return db.RuntimeResourceBinding{}, false, nil
	}
	now := time.Now().UTC()
	b.CreatedAt = now
	b.UpdatedAt = now
	s.order = append(s.order, k)
	s.byOMA[k] = b
	s.byRemote[rk] = b
	return b, true, nil
}

func (s *testBindingStore) GetRuntimeResourceBindingByOMAID(_ context.Context, org, ws, rt, id string) (db.RuntimeResourceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.byOMA[makeOMAKey(org, ws, rt, id)]
	if !ok || b.State == "deleted" {
		return db.RuntimeResourceBinding{}, db.ErrNotFound
	}
	return b, nil
}

func (s *testBindingStore) GetRuntimeResourceBindingByRemoteID(_ context.Context, profile, rt, id string) (db.RuntimeResourceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.byRemote[makeRemoteKey(profile, rt, id)]
	if !ok || b.State == "deleted" {
		return db.RuntimeResourceBinding{}, db.ErrNotFound
	}
	return b, nil
}

func (s *testBindingStore) ListRuntimeResourceBindingsByWorkspaceAndType(_ context.Context, p db.ListRuntimeResourceBindingsParams) ([]db.RuntimeResourceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var matched []db.RuntimeResourceBinding
	for i := len(s.order) - 1; i >= 0; i-- {
		b := s.byOMA[s.order[i]]
		if b.OrganizationUUID != p.OrganizationUUID || b.WorkspaceUUID != p.WorkspaceUUID || b.ResourceType != p.ResourceType {
			continue
		}
		if b.State == "deleted" {
			continue
		}
		if !p.IncludeArchived && b.State == "archived" {
			continue
		}
		if p.ParentOMAExternalID != "" && b.ParentOMAExternalID != p.ParentOMAExternalID {
			continue
		}
		if p.ResourceRole != "" && b.ResourceRole != p.ResourceRole {
			continue
		}
		matched = append(matched, b)
	}
	if p.Offset >= len(matched) {
		return nil, nil
	}
	end := p.Offset + p.Limit
	if end > len(matched) || p.Limit <= 0 {
		end = len(matched)
	}
	return matched[p.Offset:end], nil
}

func (s *testBindingStore) SearchRuntimeResourceBindingsByWorkspaceAndType(_ context.Context, p db.SearchRuntimeResourceBindingsParams) ([]db.RuntimeResourceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var matched []db.RuntimeResourceBinding
	q := strings.ToLower(p.Query)
	for i := len(s.order) - 1; i >= 0; i-- {
		b := s.byOMA[s.order[i]]
		if b.OrganizationUUID != p.OrganizationUUID || b.WorkspaceUUID != p.WorkspaceUUID || b.ResourceType != p.ResourceType {
			continue
		}
		if b.State == "deleted" || (!p.IncludeArchived && b.State == "archived") {
			continue
		}
		if strings.Contains(strings.ToLower(string(b.Summary)), q) || strings.Contains(strings.ToLower(b.OMAExternalID), q) {
			matched = append(matched, b)
		}
	}
	if p.Offset >= len(matched) {
		return nil, nil
	}
	end := p.Offset + p.Limit
	if end > len(matched) || p.Limit <= 0 {
		end = len(matched)
	}
	return matched[p.Offset:end], nil
}

func (s *testBindingStore) UpdateRuntimeResourceBindingSummaryAndState(_ context.Context, org, ws, rt, id, state string, summary json.RawMessage) (db.RuntimeResourceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := makeOMAKey(org, ws, rt, id)
	b, ok := s.byOMA[k]
	if !ok || b.State == "deleted" {
		return db.RuntimeResourceBinding{}, db.ErrNotFound
	}
	if state != "" {
		b.State = state
	}
	if len(summary) > 0 {
		b.Summary = summary
	}
	b.UpdatedAt = time.Now().UTC()
	s.byOMA[k] = b
	s.byRemote[makeRemoteKey(b.RuntimeProfileUUID, b.ResourceType, b.RemoteID)] = b
	return b, nil
}

func (s *testBindingStore) SoftDeleteRuntimeResourceBinding(_ context.Context, org, ws, rt, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := makeOMAKey(org, ws, rt, id)
	b, ok := s.byOMA[k]
	if !ok || b.State == "deleted" {
		return db.ErrNotFound
	}
	b.State = "deleted"
	s.byOMA[k] = b
	s.byRemote[makeRemoteKey(b.RuntimeProfileUUID, b.ResourceType, b.RemoteID)] = b
	return nil
}

type testUsageRecorder struct {
	mu      sync.Mutex
	records []db.ProviderUsageRecord
}

func (u *testUsageRecorder) RecordProviderUsage(_ context.Context, record db.ProviderUsageRecord) (db.ProviderUsageRecord, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.records = append(u.records, record)
	return record, nil
}

func (u *testUsageRecorder) snapshot() []db.ProviderUsageRecord {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]db.ProviderUsageRecord, len(u.records))
	copy(out, u.records)
	return out
}

func newTestRuntime(t *testing.T, endpoint string, profileCfg json.RawMessage) (*Runtime, *testBindingStore, *testUsageRecorder) {
	t.Helper()
	store := newTestBindingStore()
	usage := &testUsageRecorder{}
	factory := NewFactory(agentruntime.NewBindingService(store), usage, nil)
	rawRT, err := factory(context.Background(), agentruntime.ProfileRuntimeConfig{
		Profile: db.RuntimeProfile{
			UUID:         "prof-shared-qoder",
			ProviderKind: string(agentruntime.ProviderQoderCloud),
			EndpointURL:  endpoint,
			Config:       profileCfg,
		},
		Credentials:  []byte(`{"token":"test-token"}`),
		Capabilities: agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud),
	})
	if err != nil {
		t.Fatalf("newTestRuntime: %v", err)
	}
	rt := rawRT.(*Runtime)
	t.Cleanup(func() { _ = rt.Close() })
	return rt, store, usage
}

func TestSharedPoolCrossTenantIDORIsolation(t *testing.T) {
	var upstreamHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/agents":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			meta := req["metadata"].(map[string]any)
			if meta[watermarkOrgKey] != "org-a" || meta[watermarkWorkspaceKey] != "ws-a" {
				t.Errorf("missing watermark metadata: %+v", meta)
			}
			_, _ = w.Write([]byte(`{"id":"agt_remote_1","version":1,"name":"Alpha Agent","model":"qmodel_latest","tools":[{"type":"agent_toolset_20260401","configs":[{"name":"DeliverArtifacts","enabled":true}]}],"metadata":{"oma_org_uuid":"org-a","oma_workspace_id":"ws-a","team":"core"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/agents/agt_remote_1":
			_, _ = w.Write([]byte(`{"id":"agt_remote_1","version":1,"name":"Alpha Agent","model":"qmodel_latest"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/environments":
			_, _ = w.Write([]byte(`{"id":"env_remote_1","name":"Alpha Env","config":{"type":"cloud"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/files":
			_, _ = w.Write([]byte(`{"id":"file_remote_1","filename":"notes.txt","mime_type":"text/plain","size_bytes":5,"downloadable":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			_, _ = w.Write([]byte(`{"id":"sess_remote_1","agent":"agt_remote_1","environment_id":"env_remote_1","title":"Alpha Session","status":"running"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt, _, _ := newTestRuntime(t, srv.URL, nil)
	ctx := context.Background()

	scopeA := agentruntime.Scope{
		OrganizationUUID:   "org-a",
		WorkspaceUUID:      "ws-a",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	scopeB := agentruntime.Scope{
		OrganizationUUID:   "org-b",
		WorkspaceUUID:      "ws-b",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}

	ag, err := rt.CreateAgent(ctx, scopeA, agentruntime.CreateAgentInput{
		Name:     "Alpha Agent",
		Model:    "qmodel_latest",
		Metadata: map[string]string{"team": "core"},
	})
	if err != nil || !strings.HasPrefix(ag.ID, "agent_") || ag.Metadata[watermarkOrgKey] != "" || ag.Metadata["team"] != "core" {
		t.Fatalf("CreateAgent = (%+v, %v)", ag, err)
	}
	env, err := rt.CreateEnvironment(ctx, scopeA, agentruntime.CreateEnvironmentInput{Name: "Alpha Env"})
	if err != nil || !strings.HasPrefix(env.ID, "env_") {
		t.Fatalf("CreateEnvironment = (%+v, %v)", env, err)
	}
	fileObj, err := rt.UploadFile(ctx, scopeA, agentruntime.UploadFileInput{
		Filename: "notes.txt",
		MimeType: "text/plain",
		Content:  []byte("hello"),
	})
	if err != nil || !strings.HasPrefix(fileObj.ID, "file_") {
		t.Fatalf("UploadFile = (%+v, %v)", fileObj, err)
	}
	sess, err := rt.CreateSession(ctx, scopeA, agentruntime.CreateSessionInput{
		AgentID:       ag.ID,
		EnvironmentID: env.ID,
		Title:         "Alpha Session",
	})
	if err != nil || !strings.HasPrefix(sess.ID, "sesn_") {
		t.Fatalf("CreateSession = (%+v, %v)", sess, err)
	}

	hitsBeforeIDOR := upstreamHits.Load()

	assertNotFound := func(name string, err error) {
		t.Helper()
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Kind != apperr.NotFound {
			t.Fatalf("%s err = %v, want apperr.NotFound", name, err)
		}
	}

	_, err = rt.GetAgent(ctx, scopeB, ag.ID)
	assertNotFound("GetAgent", err)
	_, err = rt.UpdateAgent(ctx, scopeB, ag.ID, agentruntime.UpdateAgentInput{Version: 1})
	assertNotFound("UpdateAgent", err)
	_, err = rt.ArchiveAgent(ctx, scopeB, ag.ID)
	assertNotFound("ArchiveAgent", err)
	_, err = rt.GetEnvironment(ctx, scopeB, env.ID)
	assertNotFound("GetEnvironment", err)
	err = rt.DeleteEnvironment(ctx, scopeB, env.ID)
	assertNotFound("DeleteEnvironment", err)
	_, err = rt.GetFile(ctx, scopeB, fileObj.ID)
	assertNotFound("GetFile", err)
	_, err = rt.DownloadFile(ctx, scopeB, fileObj.ID)
	assertNotFound("DownloadFile", err)
	err = rt.DeleteFile(ctx, scopeB, fileObj.ID)
	assertNotFound("DeleteFile", err)
	_, err = rt.GetSession(ctx, scopeB, sess.ID)
	assertNotFound("GetSession", err)
	_, err = rt.SendEvents(ctx, scopeB, sess.ID, agentruntime.SendEventsInput{})
	assertNotFound("SendEvents", err)
	_, err = rt.ListEvents(ctx, scopeB, sess.ID, agentruntime.ListEventsQuery{})
	assertNotFound("ListEvents", err)
	_, err = rt.StreamEvents(ctx, scopeB, sess.ID, agentruntime.StreamOptions{})
	assertNotFound("StreamEvents", err)
	_, err = rt.ListSessionResources(ctx, scopeB, sess.ID)
	assertNotFound("ListSessionResources", err)
	_, err = rt.ListThreads(ctx, scopeB, sess.ID)
	assertNotFound("ListThreads", err)
	_, err = rt.CreateSession(ctx, scopeB, agentruntime.CreateSessionInput{AgentID: ag.ID})
	assertNotFound("CreateSession cross-tenant agent", err)
	_, err = rt.ListFiles(ctx, scopeB, agentruntime.ListFilesQuery{ScopeID: sess.ID})
	assertNotFound("ListFiles cross-tenant scope", err)

	if upstreamHits.Load() != hitsBeforeIDOR {
		t.Fatalf("cross-tenant IDOR leaked %d requests to upstream Qoder", upstreamHits.Load()-hitsBeforeIDOR)
	}

	bAgents, err := rt.ListAgents(ctx, scopeB, agentruntime.ListAgentsQuery{})
	if err != nil || len(bAgents.Items) != 0 {
		t.Fatalf("scopeB ListAgents = (%+v, %v), want empty", bAgents, err)
	}
	bSearch, err := rt.SearchAgents(ctx, scopeB, agentruntime.SearchAgentsQuery{Query: "Alpha"})
	if err != nil || len(bSearch.Items) != 0 {
		t.Fatalf("scopeB SearchAgents = (%+v, %v), want empty", bSearch, err)
	}
	aSearch, err := rt.SearchAgents(ctx, scopeA, agentruntime.SearchAgentsQuery{Query: "Alpha"})
	if err != nil || len(aSearch.Items) != 1 || aSearch.Items[0].ID != ag.ID {
		t.Fatalf("scopeA SearchAgents = (%+v, %v)", aSearch, err)
	}
}

func TestDownloadFileSSRFProtection(t *testing.T) {
	var currentDownloadURL string
	qoderSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/files/file_remote_ssrf/content" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url":        currentDownloadURL,
				"expires_at": time.Now().UTC().Add(5 * time.Minute),
				"filename":   "artifact.txt",
				"mime_type":  "text/plain",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer qoderSrv.Close()

	rt, store, _ := newTestRuntime(t, qoderSrv.URL, nil)
	ctx := context.Background()
	scope := agentruntime.Scope{
		OrganizationUUID:   "org-1",
		WorkspaceUUID:      "ws-1",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	_, _ = store.UpsertRuntimeResourceBinding(ctx, db.RuntimeResourceBinding{
		OrganizationUUID:   scope.OrganizationUUID,
		WorkspaceUUID:      scope.WorkspaceUUID,
		RuntimeProfileUUID: scope.RuntimeProfileUUID,
		ProviderKind:       scope.ProviderKind.String(),
		ResourceType:       string(agentruntime.ResourceTypeFile),
		OMAExternalID:      "file_oma_ssrf",
		RemoteID:           "file_remote_ssrf",
		State:              "active",
	})

	blockedURLs := []struct {
		name   string
		target string
		ips    []net.IP
	}{
		{
			name:   "http_scheme",
			target: "http://cloud-agents-storage-cn.oss-cn-beijing.aliyuncs.com/obj",
			ips:    []net.IP{net.ParseIP("8.8.8.8")},
		},
		{
			name:   "loopback_ip_literal",
			target: "https://127.0.0.1/secret",
			ips:    []net.IP{net.ParseIP("127.0.0.1")},
		},
		{
			name:   "metadata_ip_literal",
			target: "https://169.254.169.254/latest/meta-data/",
			ips:    []net.IP{net.ParseIP("169.254.169.254")},
		},
		{
			name:   "disallowed_domain",
			target: "https://evil.example.com/file.txt",
			ips:    []net.IP{net.ParseIP("93.184.216.34")},
		},
		{
			name:   "dns_resolves_loopback",
			target: "https://cloud-agents-storage-cn.oss-cn-beijing.aliyuncs.com/obj",
			ips:    []net.IP{net.ParseIP("127.0.0.1")},
		},
		{
			name:   "dns_resolves_metadata",
			target: "https://bucket.oss-cn-shanghai.aliyuncs.com/obj",
			ips:    []net.IP{net.ParseIP("169.254.169.254")},
		},
		{
			name:   "dns_resolves_rfc1918",
			target: "https://bucket.oss-cn-hangzhou.aliyuncs.com/obj",
			ips:    []net.IP{net.ParseIP("10.0.0.5")},
		},
		{
			name:   "dns_resolves_ipv6_loopback",
			target: "https://cloud-agents-storage-cn.oss-cn-beijing.aliyuncs.com/obj",
			ips:    []net.IP{net.ParseIP("::1")},
		},
	}

	for _, tc := range blockedURLs {
		t.Run(tc.name, func(t *testing.T) {
			currentDownloadURL = tc.target
			rt.lookupIP = func(context.Context, string) ([]net.IP, error) {
				return tc.ips, nil
			}
			stream, err := rt.DownloadFile(ctx, scope, "file_oma_ssrf")
			if stream != nil {
				_ = stream.Close()
			}
			if !errors.Is(err, ErrDownloadBlocked) {
				t.Fatalf("DownloadFile(%s) err = %v, want ErrDownloadBlocked", tc.name, err)
			}
		})
	}

	t.Run("cross_origin_redirect_blocked", func(t *testing.T) {
		redirectSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://other-bucket.oss-cn-shanghai.aliyuncs.com/stolen", http.StatusFound)
		}))
		defer redirectSrv.Close()

		parsedRedirect, _ := url.Parse(redirectSrv.URL)
		rtRedirect, storeRedirect, _ := newTestRuntime(t, qoderSrv.URL, json.RawMessage(`{"allowed_download_hosts":["download.allowed.test"]}`))
		_, _ = storeRedirect.UpsertRuntimeResourceBinding(ctx, db.RuntimeResourceBinding{
			OrganizationUUID:   scope.OrganizationUUID,
			WorkspaceUUID:      scope.WorkspaceUUID,
			RuntimeProfileUUID: scope.RuntimeProfileUUID,
			ProviderKind:       scope.ProviderKind.String(),
			ResourceType:       string(agentruntime.ResourceTypeFile),
			OMAExternalID:      "file_oma_ssrf",
			RemoteID:           "file_remote_ssrf",
			State:              "active",
		})
		rtRedirect.lookupIP = func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		tlsClient := redirectSrv.Client()
		tlsTransport := tlsClient.Transport.(*http.Transport).Clone()
		tlsTransport.TLSClientConfig.ServerName = "example.com"
		tlsTransport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, parsedRedirect.Host)
		}
		rtRedirect.downloadClient.Transport = tlsTransport

		currentDownloadURL = "https://download.allowed.test/file"
		stream, err := rtRedirect.DownloadFile(ctx, scope, "file_oma_ssrf")
		if stream != nil {
			_ = stream.Close()
		}
		if !errors.Is(err, ErrDownloadBlocked) {
			t.Fatalf("cross-origin redirect err = %v, want ErrDownloadBlocked", err)
		}
	})

	t.Run("valid_streaming_download_succeeds", func(t *testing.T) {
		ossSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Errorf("Authorization header leaked to OSS download")
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", "13")
			_, _ = w.Write([]byte("streamed-data"))
		}))
		defer ossSrv.Close()

		parsedOSS, _ := url.Parse(ossSrv.URL)
		rtOK, storeOK, _ := newTestRuntime(t, qoderSrv.URL, nil)
		_, _ = storeOK.UpsertRuntimeResourceBinding(ctx, db.RuntimeResourceBinding{
			OrganizationUUID:   scope.OrganizationUUID,
			WorkspaceUUID:      scope.WorkspaceUUID,
			RuntimeProfileUUID: scope.RuntimeProfileUUID,
			ProviderKind:       scope.ProviderKind.String(),
			ResourceType:       string(agentruntime.ResourceTypeFile),
			OMAExternalID:      "file_oma_ssrf",
			RemoteID:           "file_remote_ssrf",
			State:              "active",
		})
		rtOK.lookupIP = func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		tlsTransport := ossSrv.Client().Transport.(*http.Transport).Clone()
		tlsTransport.TLSClientConfig.ServerName = "example.com"
		tlsTransport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, parsedOSS.Host)
		}
		rtOK.downloadClient.Transport = tlsTransport

		currentDownloadURL = "https://cloud-agents-storage-cn.oss-cn-beijing.aliyuncs.com/signed/artifact.txt?signature=redacted"
		stream, err := rtOK.DownloadFile(ctx, scope, "file_oma_ssrf")
		if err != nil {
			t.Fatalf("DownloadFile(valid) err = %v", err)
		}
		defer stream.Close()
		body, err := io.ReadAll(stream)
		if err != nil {
			t.Fatalf("read stream: %v", err)
		}
		if string(body) != "streamed-data" || stream.ContentType() != "text/plain" || stream.ContentLength() != 13 || stream.Filename() != "artifact.txt" {
			t.Fatalf("stream = (%q, %q, %d, %q)", string(body), stream.ContentType(), stream.ContentLength(), stream.Filename())
		}
	})
}

func TestDeleteFileTwoStepDetachOn409AndClaimOutputs(t *testing.T) {
	detached := false
	deleteCalls := 0
	returnRefSessions := true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/files" && r.URL.Query().Get("scope_id") == "sess_remote_out":
			_, _ = w.Write([]byte(`{"data":[{"id":"file_remote_out_1","filename":"report.md","mime_type":"text/markdown","size_bytes":42,"downloadable":true}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/files/file_remote_out_1":
			deleteCalls++
			if !detached {
				w.WriteHeader(http.StatusConflict)
				if returnRefSessions {
					_, _ = w.Write([]byte(`{"error":{"type":"conflict_error","message":"file referenced by session","referenced_session_ids":["sess_remote_out"]}}`))
				} else {
					_, _ = w.Write([]byte(`{"error":{"type":"conflict_error","message":"file referenced by session"}}`))
				}
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"deleted":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/sessions/sess_remote_out/resources":
			_, _ = w.Write([]byte(`{"data":[{"id":"sesr_attach_1","type":"file","file_id":"file_remote_out_1","mount_path":"/data/report.md"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/sessions/sess_remote_out/resources/sesr_attach_1":
			detached = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"deleted":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt, store, _ := newTestRuntime(t, srv.URL, nil)
	ctx := context.Background()
	scope := agentruntime.Scope{
		OrganizationUUID:   "org-1",
		WorkspaceUUID:      "ws-1",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	_, _ = store.UpsertRuntimeResourceBinding(ctx, db.RuntimeResourceBinding{
		OrganizationUUID:   scope.OrganizationUUID,
		WorkspaceUUID:      scope.WorkspaceUUID,
		RuntimeProfileUUID: scope.RuntimeProfileUUID,
		ProviderKind:       scope.ProviderKind.String(),
		ResourceType:       string(agentruntime.ResourceTypeSession),
		OMAExternalID:      "sesn_oma_out",
		RemoteID:           "sess_remote_out",
		State:              "active",
	})

	page, err := rt.ListFiles(ctx, scope, agentruntime.ListFilesQuery{ScopeID: "sesn_oma_out"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Role != "session_output" || page.Items[0].ScopeID != "sesn_oma_out" {
		t.Fatalf("ListFiles(ScopeID) = (%+v, %v)", page, err)
	}
	claimedID := page.Items[0].ID

	pageAgain, err := rt.ListFiles(ctx, scope, agentruntime.ListFilesQuery{ScopeID: "sesn_oma_out"})
	if err != nil || len(pageAgain.Items) != 1 || pageAgain.Items[0].ID != claimedID {
		t.Fatalf("ListFiles(ScopeID) idempotent claim = (%+v, %v)", pageAgain, err)
	}

	if err := rt.DeleteFile(ctx, scope, claimedID); err != nil {
		t.Fatalf("DeleteFile(409 with referenced_session_ids): %v", err)
	}
	if !detached || deleteCalls != 2 {
		t.Fatalf("expected two-step detach+retry, detached=%v deleteCalls=%d", detached, deleteCalls)
	}
	if _, err := rt.GetFile(ctx, scope, claimedID); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("GetFile after delete err = %v, want ErrResourceNotFound", err)
	}

	detached = false
	deleteCalls = 0
	returnRefSessions = false
	_, _ = store.UpsertRuntimeResourceBinding(ctx, db.RuntimeResourceBinding{
		OrganizationUUID:    scope.OrganizationUUID,
		WorkspaceUUID:       scope.WorkspaceUUID,
		RuntimeProfileUUID:  scope.RuntimeProfileUUID,
		ProviderKind:        scope.ProviderKind.String(),
		ResourceType:        string(agentruntime.ResourceTypeFile),
		OMAExternalID:       "file_oma_parent_fallback",
		RemoteID:            "file_remote_out_1",
		ParentOMAExternalID: "sesn_oma_out",
		ResourceRole:        "session_output",
		State:               "active",
	})
	if err := rt.DeleteFile(ctx, scope, "file_oma_parent_fallback"); err != nil {
		t.Fatalf("DeleteFile(409 parent fallback): %v", err)
	}
	if !detached || deleteCalls != 2 {
		t.Fatalf("expected parent fallback detach+retry, detached=%v deleteCalls=%d", detached, deleteCalls)
	}
}

func TestUploadFileConstraintsAndSessionBudgetAndListEventsPagination(t *testing.T) {
	var receivedCreateSession map[string]any
	eventPageCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/files":
			_, _ = w.Write([]byte(`{"id":"file_remote_png","filename":"pixel.png","mime_type":"image/png","size_bytes":8,"downloadable":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			_ = json.NewDecoder(r.Body).Decode(&receivedCreateSession)
			_, _ = w.Write([]byte(`{"id":"sess_remote_1","agent":"agt_remote_1","title":"Budget Session","status":"running","budget":{"type":"limit","max_credit_cost":"100.00"},"usage":{"model_credits":1.5,"sandbox_runtime_credits":0.5,"total_credits":2.0}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/sessions/sess_remote_1":
			_, _ = w.Write([]byte(`{"id":"sess_remote_1","agent":"agt_remote_1","title":"Budget Session","status":"idle","usage":{"model_credits":3.0,"sandbox_runtime_credits":1.0,"total_credits":4.0}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/sessions/sess_remote_1/events":
			eventPageCalls++
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			after := r.URL.Query().Get("after_id")
			startIdx := 1
			if after != "" {
				n, _ := strconv.Atoi(strings.TrimPrefix(after, "evt_"))
				startIdx = n + 1
			}
			total := 230
			endIdx := startIdx + limit - 1
			if endIdx > total {
				endIdx = total
			}
			data := make([]map[string]any, 0, endIdx-startIdx+1)
			for i := startIdx; i <= endIdx; i++ {
				data = append(data, map[string]any{
					"id":   fmt.Sprintf("evt_%d", i),
					"type": "agent.message",
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":     data,
				"first_id": fmt.Sprintf("evt_%d", startIdx),
				"last_id":  fmt.Sprintf("evt_%d", endIdx),
				"has_more": endIdx < total,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt, store, usage := newTestRuntime(t, srv.URL, nil)
	ctx := context.Background()
	scope := agentruntime.Scope{
		OrganizationUUID:   "org-1",
		WorkspaceUUID:      "ws-1",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	_, _ = store.UpsertRuntimeResourceBinding(ctx, db.RuntimeResourceBinding{
		OrganizationUUID:   scope.OrganizationUUID,
		WorkspaceUUID:      scope.WorkspaceUUID,
		RuntimeProfileUUID: scope.RuntimeProfileUUID,
		ProviderKind:       scope.ProviderKind.String(),
		ResourceType:       string(agentruntime.ResourceTypeAgent),
		OMAExternalID:      "agent_oma_1",
		RemoteID:           "agt_remote_1",
		State:              "active",
		Summary:            json.RawMessage(`{"name":"Agent One"}`),
	})

	oversized := make([]byte, 5*1024*1024+1)
	if _, err := rt.UploadFile(ctx, scope, agentruntime.UploadFileInput{
		Filename: "huge.txt",
		MimeType: "text/plain",
		Content:  oversized,
	}); err == nil {
		t.Fatal("expected oversized upload error")
	}
	if _, err := rt.UploadFile(ctx, scope, agentruntime.UploadFileInput{
		Filename: "bin.exe",
		MimeType: "application/octet-stream",
		Content:  []byte{0x00, 0x01, 0x02},
	}); err == nil {
		t.Fatal("expected disallowed binary MIME error")
	}
	pngFile, err := rt.UploadFile(ctx, scope, agentruntime.UploadFileInput{
		Filename: "pixel.png",
		MimeType: "image/png",
		Content:  []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
	})
	if err != nil || pngFile.Filename != "pixel.png" {
		t.Fatalf("UploadFile(image/png) = (%+v, %v)", pngFile, err)
	}

	if _, err := rt.CreateSession(ctx, scope, agentruntime.CreateSessionInput{
		AgentID: "agent_oma_1",
		Budget:  &agentruntime.SessionBudget{Mode: agentruntime.BudgetModeUSD, MaxCostUSD: "5.00"},
	}); !errors.Is(err, agentruntime.ErrCapabilityUnsupported) {
		t.Fatalf("CreateSession(USD budget) err = %v, want ErrCapabilityUnsupported", err)
	}

	sess, err := rt.CreateSession(ctx, scope, agentruntime.CreateSessionInput{
		AgentID:       "agent_oma_1",
		Title:         "Budget Session",
		Budget:        &agentruntime.SessionBudget{Mode: agentruntime.BudgetModeCredits, MaxCreditCost: "100.00"},
		InitialEvents: []json.RawMessage{json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"go"}]}`)},
	})
	if err != nil || sess.Budget == nil || sess.Budget.MaxCreditCost != "100.00" {
		t.Fatalf("CreateSession(credits) = (%+v, %v)", sess, err)
	}
	budgetMap := receivedCreateSession["budget"].(map[string]any)
	if budgetMap["type"] != "limit" || budgetMap["max_credit_cost"] != "100.00" {
		t.Fatalf("forwarded budget = %+v", budgetMap)
	}
	if len(receivedCreateSession["initial_events"].([]any)) != 1 {
		t.Fatalf("initial_events not forwarded: %+v", receivedCreateSession["initial_events"])
	}

	if _, err := rt.GetSession(ctx, scope, sess.ID); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	records := usage.snapshot()
	if len(records) != 2 || records[0].CumulativeAmount != 2.0 || records[0].DeltaAmount != 2.0 || records[1].CumulativeAmount != 4.0 || records[1].DeltaAmount != 2.0 {
		t.Fatalf("recorded usage = %+v", records)
	}

	if _, err := rt.GetSession(ctx, scope, sess.ID); err != nil {
		t.Fatalf("GetSession(repeat): %v", err)
	}
	if len(usage.snapshot()) != 2 {
		t.Fatalf("expected idempotent usage skip when total unchanged, got %d records", len(usage.snapshot()))
	}

	eventsPage, err := rt.ListEvents(ctx, scope, sess.ID, agentruntime.ListEventsQuery{Limit: 250})
	if err != nil {
		t.Fatalf("ListEvents(limit=250): %v", err)
	}
	if len(eventsPage.Items) != 230 || eventPageCalls != 3 || eventsPage.FirstID != "sevt_qdr_1" || eventsPage.LastID != "sevt_qdr_230" || eventsPage.HasMore {
		t.Fatalf("ListEvents = (len=%d, calls=%d, first=%q, last=%q, hasMore=%v)", len(eventsPage.Items), eventPageCalls, eventsPage.FirstID, eventsPage.LastID, eventsPage.HasMore)
	}
}

func TestStreamEventsRoutingHubDeltaFilterThreadIsolationAndLastEventID(t *testing.T) {
	var (
		mu             sync.Mutex
		hubConnections = make(map[string]int)
		directCalls    int
		lastSeenHeader string
		seenDeltas     []string
		upstreamClosed atomic.Int32
	)
	releaseHub := make(chan struct{})
	mainStreamReady := make(chan struct{}, 4)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/sess_remote_sse/events/stream" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		lastID := r.Header.Get("Last-Event-ID")
		threadID := r.URL.Query().Get("session_thread_id")
		deltas := r.URL.Query()["event_deltas[]"]

		if lastID != "" {
			mu.Lock()
			directCalls++
			lastSeenHeader = lastID
			mu.Unlock()
			if lastID == "evt_invalid" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"type":"not_found_error","message":"Event not found"}}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			_, _ = fmt.Fprint(w, "id: evt_replay_1\nevent: agent.message\ndata: {\"id\":\"evt_replay_1\",\"type\":\"agent.message\"}\n\n")
			flusher.Flush()
			return
		}

		mu.Lock()
		hubConnections[threadID]++
		if threadID == "" {
			seenDeltas = append([]string(nil), deltas...)
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		if threadID == "sthr_sub_1" {
			_, _ = fmt.Fprint(w, "id: evt_thread_1\nevent: agent.message\ndata: {\"id\":\"evt_thread_1\",\"type\":\"agent.message\",\"session_thread_id\":\"sthr_sub_1\"}\n\n")
			flusher.Flush()
			<-r.Context().Done()
			upstreamClosed.Add(1)
			return
		}

		select {
		case mainStreamReady <- struct{}{}:
		default:
		}
		<-releaseHub

		frames := []string{
			"id: evt_p_msg\nevent: event_start\ndata: {\"type\":\"event_start\",\"event\":{\"id\":\"evt_p_msg\",\"type\":\"agent.message\"}}\n\n",
			"id: evt_p_msg\nevent: event_delta\ndata: {\"type\":\"event_delta\",\"event_id\":\"evt_p_msg\",\"delta\":{\"type\":\"content_delta\",\"content\":{\"type\":\"text\",\"text\":\"hello\"}}}\n\n",
			"id: evt_p_thk\nevent: event_start\ndata: {\"type\":\"event_start\",\"event\":{\"id\":\"evt_p_thk\",\"type\":\"agent.thinking\"}}\n\n",
			"id: evt_p_thk\nevent: event_delta\ndata: {\"type\":\"event_delta\",\"event_id\":\"evt_p_thk\",\"delta\":{\"type\":\"content_delta\",\"content\":{\"type\":\"thinking\",\"thinking\":\"secret\"}}}\n\n",
			"id: evt_u_1\nevent: session.usage\ndata: {\"id\":\"evt_u_1\",\"type\":\"session.usage\",\"model_credits\":1.2,\"sandbox_runtime_credits\":0.3,\"total_credits\":1.5}\n\n",
			"id: evt_idle_1\nevent: session.status_idle\ndata: {\"id\":\"evt_idle_1\",\"type\":\"session.status_idle\",\"stop_reason\":{\"type\":\"end_turn\",\"event_ids\":[\"evt_p_msg\"]}}\n\n",
		}
		for _, f := range frames {
			_, _ = fmt.Fprint(w, f)
			flusher.Flush()
		}
		<-r.Context().Done()
		upstreamClosed.Add(1)
	}))
	defer srv.Close()

	rt, store, usage := newTestRuntime(t, srv.URL, nil)
	defer func() { _ = rt.Close() }()
	rt.hub.idleTimeout = 80 * time.Millisecond
	ctx := context.Background()
	scope := agentruntime.Scope{
		OrganizationUUID:   "org-1",
		WorkspaceUUID:      "ws-1",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	_, _ = store.UpsertRuntimeResourceBinding(ctx, db.RuntimeResourceBinding{
		OrganizationUUID:   scope.OrganizationUUID,
		WorkspaceUUID:      scope.WorkspaceUUID,
		RuntimeProfileUUID: scope.RuntimeProfileUUID,
		ProviderKind:       scope.ProviderKind.String(),
		ResourceType:       string(agentruntime.ResourceTypeSession),
		OMAExternalID:      "sesn_oma_sse",
		RemoteID:           "sess_remote_sse",
		State:              "active",
	})

	if _, err := rt.StreamEvents(ctx, scope, "sesn_oma_sse", agentruntime.StreamOptions{LastEventID: "sevt_qdr_invalid"}); err == nil {
		t.Fatal("expected 404 on invalid Last-Event-ID")
	} else {
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Kind != apperr.NotFound {
			t.Fatalf("invalid Last-Event-ID err = %v, want NotFound", err)
		}
	}

	directStream, err := rt.StreamEvents(ctx, scope, "sesn_oma_sse", agentruntime.StreamOptions{LastEventID: "sevt_qdr_10"})
	if err != nil {
		t.Fatalf("StreamEvents(direct): %v", err)
	}
	replayed, err := directStream.Next(ctx)
	if err != nil || replayed.ID != "sevt_qdr_replay_1" {
		t.Fatalf("direct replay frame = (%+v, %v)", replayed, err)
	}
	_ = directStream.Close()

	mu.Lock()
	if directCalls != 2 || lastSeenHeader != "evt_10" {
		mu.Unlock()
		t.Fatalf("directCalls=%d lastSeenHeader=%q", directCalls, lastSeenHeader)
	}
	mu.Unlock()

	subMsgOnly, err := rt.StreamEvents(ctx, scope, "sesn_oma_sse", agentruntime.StreamOptions{DeltaTypes: []string{"agent.message"}})
	if err != nil {
		t.Fatalf("subscribe subMsgOnly: %v", err)
	}
	<-mainStreamReady

	subBoth, err := rt.StreamEvents(ctx, scope, "sesn_oma_sse", agentruntime.StreamOptions{DeltaTypes: []string{"agent.message", "agent.thinking"}})
	if err != nil {
		t.Fatalf("subscribe subBoth: %v", err)
	}
	subNoDelta, err := rt.StreamEvents(ctx, scope, "sesn_oma_sse", agentruntime.StreamOptions{})
	if err != nil {
		t.Fatalf("subscribe subNoDelta: %v", err)
	}
	subThread, err := rt.StreamEvents(ctx, scope, "sesn_oma_sse", agentruntime.StreamOptions{ThreadID: "sthr_sub_1"})
	if err != nil {
		t.Fatalf("subscribe subThread: %v", err)
	}

	close(releaseHub)

	readN := func(stream agentruntime.EventStream, count int) []agentruntime.StreamFrame {
		t.Helper()
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out := make([]agentruntime.StreamFrame, 0, count)
		for range count {
			f, err := stream.Next(readCtx)
			if err != nil {
				t.Fatalf("Next(%d): %v", len(out), err)
			}
			out = append(out, f)
		}
		return out
	}

	msgFrames := readN(subMsgOnly, 4)
	bothFrames := readN(subBoth, 6)
	noDeltaFrames := readN(subNoDelta, 2)
	threadFrames := readN(subThread, 1)

	for _, f := range msgFrames {
		if strings.Contains(string(f.Data), "agent.thinking") || strings.Contains(string(f.Data), `"secret"`) {
			t.Fatalf("subMsgOnly leaked thinking frame: %+v", f)
		}
	}
	if bothFrames[2].Event != "event_start" || bothFrames[3].Event != "event_delta" || bothFrames[3].ID != "sevt_qdr_p_thk" {
		t.Fatalf("subBoth missing thinking frames: %+v", bothFrames)
	}
	if noDeltaFrames[0].Event != "session.usage" || noDeltaFrames[1].Event != "session.status_idle" {
		t.Fatalf("subNoDelta frames = %+v", noDeltaFrames)
	}
	if !strings.Contains(string(noDeltaFrames[1].Data), "sevt_qdr_p_msg") {
		t.Fatalf("status_idle stop_reason.event_ids not encoded: %s", noDeltaFrames[1].Data)
	}
	if threadFrames[0].ID != "sevt_qdr_thread_1" {
		t.Fatalf("subThread frame = %+v", threadFrames[0])
	}

	mu.Lock()
	if hubConnections[""] != 1 || hubConnections["sthr_sub_1"] != 1 {
		mu.Unlock()
		t.Fatalf("hubConnections = %+v, want 1 per thread key", hubConnections)
	}
	if len(seenDeltas) != 2 || seenDeltas[0] != "agent.message" || seenDeltas[1] != "agent.thinking" {
		mu.Unlock()
		t.Fatalf("hub upstream deltas = %v", seenDeltas)
	}
	mu.Unlock()

	usageRecs := usage.snapshot()
	if len(usageRecs) != 1 || usageRecs[0].CumulativeAmount != 1.5 || usageRecs[0].MetricUnit != "credits" {
		t.Fatalf("stream usage records = %+v", usageRecs)
	}
	updatedBinding, err := store.GetRuntimeResourceBindingByOMAID(ctx, scope.OrganizationUUID, scope.WorkspaceUUID, string(agentruntime.ResourceTypeSession), "sesn_oma_sse")
	if err != nil || !strings.Contains(string(updatedBinding.Summary), `"status":"idle"`) {
		t.Fatalf("updated session summary = (%s, %v)", updatedBinding.Summary, err)
	}

	_ = subMsgOnly.Close()
	_ = subBoth.Close()
	_ = subNoDelta.Close()
	_ = subThread.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if upstreamClosed.Load() >= 2 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if upstreamClosed.Load() < 2 {
		t.Fatalf("expected idle hub streams to close after idleTimeout, closed=%d", upstreamClosed.Load())
	}
}

func TestResolveSafeDownloadIPDevFakeIPPolicy(t *testing.T) {
	ctx := context.Background()
	fakeIP := net.ParseIP("198.18.30.216")
	loopbackIP := net.ParseIP("127.0.0.1")
	metaIP := net.ParseIP("169.254.169.254")

	strictRT := &Runtime{
		lookupIP: func(context.Context, string) ([]net.IP, error) {
			return []net.IP{fakeIP}, nil
		},
	}
	if _, err := strictRT.resolveSafeDownloadIP(ctx, "cloud-agents-storage-cn.oss-cn-beijing.aliyuncs.com"); !errors.Is(err, ErrDownloadBlocked) {
		t.Fatalf("strict mode allowed fake IP: err=%v", err)
	}

	devRT := &Runtime{
		allowDevFakeIP: true,
		lookupIP: func(context.Context, string) ([]net.IP, error) {
			return []net.IP{fakeIP}, nil
		},
	}
	resolved, err := devRT.resolveSafeDownloadIP(ctx, "cloud-agents-storage-cn.oss-cn-beijing.aliyuncs.com")
	if err != nil || resolved.String() != "198.18.30.216" {
		t.Fatalf("dev mode failed to allow allowlisted host FakeIP: addr=%v err=%v", resolved, err)
	}
	if _, err := devRT.resolveSafeDownloadIP(ctx, "198.18.30.216"); !errors.Is(err, ErrDownloadBlocked) {
		t.Fatalf("dev mode must still block raw IP literal 198.18.30.216: err=%v", err)
	}

	for _, blocked := range []net.IP{loopbackIP, metaIP, net.ParseIP("10.0.0.1")} {
		devRT.lookupIP = func(context.Context, string) ([]net.IP, error) {
			return []net.IP{blocked}, nil
		}
		if _, err := devRT.resolveSafeDownloadIP(ctx, "cloud-agents-storage-cn.oss-cn-beijing.aliyuncs.com"); !errors.Is(err, ErrDownloadBlocked) {
			t.Fatalf("dev mode allowed private/loopback IP %s: err=%v", blocked, err)
		}
	}
}

func TestAgentVersionsAndRetrieveByVersion(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/agents/agt_rem_v/versions":
			if r.URL.Query().Get("limit") != "20" {
				t.Errorf("limit = %q, want 20", r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"agt_rem_v","name":"Agent V2","model":"auto","version":2,"created_at":"` + now + `","updated_at":"` + now + `"},{"id":"agt_rem_v","name":"Agent V1","model":"auto","version":1,"created_at":"` + now + `","updated_at":"` + now + `"}],"has_more":false,"next_page":null}`))
		case r.Method == http.MethodGet && r.URL.Path == "/agents/agt_rem_v" && r.URL.Query().Get("version") == "1":
			_, _ = w.Write([]byte(`{"id":"agt_rem_v","name":"Agent V1","model":"auto","version":1,"created_at":"` + now + `","updated_at":"` + now + `"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt, store, _ := newTestRuntime(t, srv.URL, nil)
	scope := agentruntime.Scope{
		OrganizationUUID:   "org-v",
		WorkspaceUUID:      "ws-v",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	if _, err := store.UpsertRuntimeResourceBinding(context.Background(), db.RuntimeResourceBinding{
		OrganizationUUID:   scope.OrganizationUUID,
		WorkspaceUUID:      scope.WorkspaceUUID,
		RuntimeProfileUUID: scope.RuntimeProfileUUID,
		ProviderKind:       scope.ProviderKind.String(),
		ResourceType:       string(agentruntime.ResourceTypeAgent),
		OMAExternalID:      "agent_oma_v",
		RemoteID:           "agt_rem_v",
		State:              "active",
	}); err != nil {
		t.Fatal(err)
	}

	page, err := rt.ListAgentVersions(context.Background(), scope, "agent_oma_v", agentruntime.ListAgentVersionsQuery{Limit: 20})
	if err != nil {
		t.Fatalf("ListAgentVersions: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].Version != 2 || page.Items[1].Version != 1 || page.Items[0].ID != "agent_oma_v" {
		t.Fatalf("unexpected versions page: %+v", page)
	}

	v1, err := rt.GetAgentVersion(context.Background(), scope, "agent_oma_v", 1)
	if err != nil {
		t.Fatalf("GetAgentVersion: %v", err)
	}
	if v1.ID != "agent_oma_v" || v1.Version != 1 || v1.Name != "Agent V1" {
		t.Fatalf("unexpected v1 snapshot: %+v", v1)
	}
}

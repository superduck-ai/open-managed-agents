package channels

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type workspaceAgentStore struct {
	scopedStore
	agents  []db.Agent
	hasMore bool
	calls   int
}

func (s *workspaceAgentStore) ListAgentsPage(_ context.Context, params db.ListAgentsPageParams) ([]db.Agent, bool, error) {
	s.calls++
	if params.WorkspaceUUID != "workspace-a" {
		return nil, false, nil
	}
	if params.Cursor != nil {
		return nil, false, nil
	}
	return s.agents, s.hasMore, nil
}

func TestWorkspaceChannelList(t *testing.T) {
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer bridge-token" || r.Header.Get("Cookie") != "" || r.URL.RawQuery != "" {
			t.Error("桥接身份或查询参数错误")
		}
		agentID := r.Header.Get("X-OMA-Agent")
		if agentID == "agent-b" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/managed-channels/settings":
			_, _ = w.Write([]byte(`{"environment_id":"env-a"}`))
		case "/api/managed-channels":
			_, _ = w.Write([]byte(`{"data":[{"id":"ch-1","name":"飞书渠道","app_secret":"secret","enabled":true}]}`))
		default:
			t.Errorf("path = %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	store := &workspaceAgentStore{agents: []db.Agent{
		{ExternalID: "agent-a", Name: "客服助手", CreatedAt: time.Now(), UUID: "agent-uuid-a"},
		{ExternalID: "agent-b", Name: "失败助手", CreatedAt: time.Now(), UUID: "agent-uuid-b"},
	}}
	router := chi.NewRouter()
	router.Mount("/channels", NewWorkspaceHandler(config.ChannelBridgeConfig{URL: upstream.URL, Token: "bridge-token"}, store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil))

	denied := httptest.NewRequest(http.MethodGet, "/channels", nil)
	deniedResult := httptest.NewRecorder()
	router.ServeHTTP(deniedResult, denied)
	if deniedResult.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", deniedResult.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/channels", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: "workspace-a"}))
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", result.Code, result.Body.String())
	}
	var page struct {
		Data []struct {
			ID            string  `json:"id"`
			Name          string  `json:"name"`
			AppSecret     string  `json:"app_secret"`
			AgentID       string  `json:"agent_id"`
			AgentName     string  `json:"agent_name"`
			EnvironmentID *string `json:"environment_id"`
		} `json:"data"`
		Incomplete bool `json:"incomplete"`
		Truncated  bool `json:"truncated"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if !page.Incomplete || page.Truncated || len(page.Data) != 1 {
		t.Fatalf("page = %+v", page)
	}
	row := page.Data[0]
	if row.ID != "ch-1" || row.Name != "飞书渠道" || row.AppSecret != "" || row.AgentID != "agent-a" || row.AgentName != "客服助手" || row.EnvironmentID == nil || *row.EnvironmentID != "env-a" {
		t.Fatalf("row = %+v", row)
	}
}

func TestWorkspaceChannelListTruncatesAgents(t *testing.T) {
	previous := maxWorkspaceAgents
	maxWorkspaceAgents = 1
	t.Cleanup(func() { maxWorkspaceAgents = previous })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/managed-channels/settings" {
			_, _ = w.Write([]byte(`{"environment_id":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"ch-1","name":"飞书渠道"}]}`))
	}))
	defer upstream.Close()
	store := &workspaceAgentStore{hasMore: true, agents: []db.Agent{{ExternalID: "agent-a", Name: "客服助手", UUID: "agent-uuid-a", CreatedAt: time.Now()}}}
	router := chi.NewRouter()
	router.Mount("/channels", NewWorkspaceHandler(config.ChannelBridgeConfig{URL: upstream.URL, Token: "bridge-token"}, store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil))
	request := httptest.NewRequest(http.MethodGet, "/channels", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: "workspace-a"}))
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", result.Code, result.Body.String())
	}
	var page struct {
		Truncated bool `json:"truncated"`
		Data      []struct {
			AgentID string `json:"agent_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if !page.Truncated || len(page.Data) != 1 || page.Data[0].AgentID != "agent-a" || store.calls != 1 {
		t.Fatalf("page = %+v calls = %d", page, store.calls)
	}
}

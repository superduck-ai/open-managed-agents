package platformapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type runtimeProfileTestStore struct {
	consoleAPIKeyScopeStore
	policy      db.OrganizationRuntimePolicy
	profiles    []db.RuntimeProfile
	createdIn   *CreateConsoleWorkspaceInput
	createdOut  ConsoleWorkspace
	extOverview []db.WorkspaceOverviewBucket
	extAgents   []db.RuntimeResourceBinding
	extCalls    int
	nativeCalls int
}

func (s *runtimeProfileTestStore) GetOrganizationRuntimePolicy(context.Context, string) (db.OrganizationRuntimePolicy, error) {
	return s.policy, nil
}

func (s *runtimeProfileTestStore) ListPlatformRuntimeProfiles(context.Context) ([]db.RuntimeProfile, error) {
	return s.profiles, nil
}

func (s *runtimeProfileTestStore) GetRuntimeProfileByUUID(_ context.Context, profileUUID string) (db.RuntimeProfile, error) {
	for _, p := range s.profiles {
		if p.UUID == profileUUID {
			return p, nil
		}
	}
	return db.RuntimeProfile{}, db.ErrNotFound
}

func (s *runtimeProfileTestStore) GetRuntimeProfileByExternalID(_ context.Context, externalID string) (db.RuntimeProfile, error) {
	for _, p := range s.profiles {
		if p.ExternalID == externalID {
			return p, nil
		}
	}
	return db.RuntimeProfile{}, db.ErrNotFound
}

func (s *runtimeProfileTestStore) CreateConsoleWorkspace(_ context.Context, input CreateConsoleWorkspaceInput) (ConsoleWorkspace, error) {
	s.createdIn = &input
	out := s.createdOut
	if out.ExternalID == "" {
		out = ConsoleWorkspace{
			UUID:               "00000000-0000-4000-8000-000000000088",
			ExternalID:         "wrkspc_created",
			OrgUUID:            input.OrgUUID,
			Name:               input.Name,
			RuntimeProfileUUID: input.RuntimeProfileUUID,
			CreatedAt:          time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC),
			UpdatedAt:          time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC),
		}
	}
	return out, nil
}

func (s *runtimeProfileTestStore) AggregateWorkspaceOverview(context.Context, db.WorkspaceOverviewQuery) ([]db.WorkspaceOverviewBucket, error) {
	s.nativeCalls++
	return nil, nil
}

func (s *runtimeProfileTestStore) CountWorkspaceOverviewAgents(context.Context, string, string) (int64, error) {
	return 0, nil
}

func (s *runtimeProfileTestStore) AggregateExternalWorkspaceOverview(context.Context, db.WorkspaceOverviewQuery) ([]db.WorkspaceOverviewBucket, error) {
	s.extCalls++
	return s.extOverview, nil
}

func (s *runtimeProfileTestStore) ListRuntimeResourceBindingsByWorkspaceAndType(_ context.Context, params db.ListRuntimeResourceBindingsParams) ([]db.RuntimeResourceBinding, error) {
	if params.Offset >= len(s.extAgents) {
		return nil, nil
	}
	end := params.Offset + params.Limit
	if end > len(s.extAgents) {
		end = len(s.extAgents)
	}
	return s.extAgents[params.Offset:end], nil
}

func TestListConsoleRuntimeProfilesFiltersAllowedActiveAndOmitsSecrets(t *testing.T) {
	const (
		orgUUID      = "00000000-0000-4000-8000-000000000002"
		qoderUUID    = "00000000-0000-4000-8000-000000000020"
		disabledUUID = "00000000-0000-4000-8000-000000000021"
		deniedUUID   = "00000000-0000-4000-8000-000000000022"
	)

	store := &runtimeProfileTestStore{
		policy: db.OrganizationRuntimePolicy{
			OrganizationUUID:          orgUUID,
			DefaultRuntimeProfileUUID: db.DefaultOMANativeRuntimeProfileUUID,
			AllowedRuntimeProfileUUIDs: []string{
				db.DefaultOMANativeRuntimeProfileUUID,
				qoderUUID,
				disabledUUID,
			},
		},
		profiles: []db.RuntimeProfile{
			{
				UUID:             db.DefaultOMANativeRuntimeProfileUUID,
				ExternalID:       "rprof_oma_native_default",
				Scope:            "platform",
				Name:             "OMA Native",
				ProviderKind:     agentruntime.ProviderOMANative.String(),
				Status:           "active",
				LastHealthStatus: "healthy",
			},
			{
				UUID:             qoderUUID,
				ExternalID:       "rprof_qoder_cn",
				Scope:            "platform",
				Name:             "Qoder CN",
				ProviderKind:     agentruntime.ProviderQoderCloud.String(),
				Status:           "active",
				LastHealthStatus: "healthy",
				CredentialHint:   "sk-secret-hint",
			},
			{
				UUID:         disabledUUID,
				ExternalID:   "rprof_disabled",
				Scope:        "platform",
				Name:         "Disabled Profile",
				ProviderKind: agentruntime.ProviderQoderCloud.String(),
				Status:       "disabled",
			},
			{
				UUID:         deniedUUID,
				ExternalID:   "rprof_other_org",
				Scope:        "platform",
				Name:         "Not Allowed",
				ProviderKind: agentruntime.ProviderQoderCloud.String(),
				Status:       "active",
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/console/organizations/"+orgUUID+"/runtime-profiles", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("orgUuid", orgUUID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	ctx = auth.WithPrincipal(ctx, auth.Principal{
		OrganizationUUID: orgUUID,
		UserUUID:         "00000000-0000-4000-8000-000000000003",
		WorkspaceAccess:  auth.WorkspaceAccess{OrganizationRole: "user"},
	})
	rec := httptest.NewRecorder()

	handleListConsoleRuntimeProfiles(store).ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-secret-hint") {
		t.Fatalf("response leaked credential hint: %s", rec.Body.String())
	}
	var resp consoleRuntimeProfilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.DefaultRuntimeProfileUUID != db.DefaultOMANativeRuntimeProfileUUID {
		t.Fatalf("default_runtime_profile_uuid = %q", resp.DefaultRuntimeProfileUUID)
	}
	if len(resp.Profiles) != 2 {
		t.Fatalf("profiles len = %d, want 2: %+v", len(resp.Profiles), resp.Profiles)
	}
	if resp.Profiles[1].ID != "rprof_qoder_cn" || resp.Profiles[1].EffectiveCapabilities.MCPTunnels {
		t.Fatalf("unexpected qoder profile summary: %+v", resp.Profiles[1])
	}
}

func TestCreateConsoleWorkspaceValidatesRuntimeWhitelist(t *testing.T) {
	const (
		orgUUID      = "00000000-0000-4000-8000-000000000002"
		qoderUUID    = "00000000-0000-4000-8000-000000000020"
		disabledUUID = "00000000-0000-4000-8000-000000000021"
		deniedUUID   = "00000000-0000-4000-8000-000000000022"
	)

	newStore := func() *runtimeProfileTestStore {
		return &runtimeProfileTestStore{
			policy: db.OrganizationRuntimePolicy{
				OrganizationUUID:           orgUUID,
				DefaultRuntimeProfileUUID:  db.DefaultOMANativeRuntimeProfileUUID,
				AllowedRuntimeProfileUUIDs: []string{db.DefaultOMANativeRuntimeProfileUUID, qoderUUID, disabledUUID},
			},
			profiles: []db.RuntimeProfile{
				{UUID: db.DefaultOMANativeRuntimeProfileUUID, ExternalID: "rprof_oma_native_default", Scope: "platform", Name: "OMA Native", ProviderKind: agentruntime.ProviderOMANative.String(), Status: "active"},
				{UUID: qoderUUID, ExternalID: "rprof_qoder_cn", Scope: "platform", Name: "Qoder CN", ProviderKind: agentruntime.ProviderQoderCloud.String(), Status: "active"},
				{UUID: disabledUUID, ExternalID: "rprof_disabled", Scope: "platform", Name: "Disabled", ProviderKind: agentruntime.ProviderQoderCloud.String(), Status: "disabled"},
				{UUID: deniedUUID, ExternalID: "rprof_denied", Scope: "platform", Name: "Denied", ProviderKind: agentruntime.ProviderQoderCloud.String(), Status: "active"},
			},
		}
	}

	postReq := func(body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/console/organizations/"+orgUUID+"/workspaces", strings.NewReader(body))
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("orgUuid", orgUUID)
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
		ctx = auth.WithPrincipal(ctx, auth.Principal{
			OrganizationUUID: orgUUID,
			UserUUID:         "00000000-0000-4000-8000-000000000003",
			WorkspaceAccess:  auth.WorkspaceAccess{OrganizationRole: "admin"},
		})
		return req.WithContext(ctx)
	}

	t.Run("accepts allowed active profile by external id and returns inline runtime", func(t *testing.T) {
		store := newStore()
		rec := httptest.NewRecorder()
		handleCreateConsoleWorkspace(store).ServeHTTP(rec, postReq(`{"name":"Qoder WS","runtime_profile_id":"rprof_qoder_cn"}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if store.createdIn == nil || store.createdIn.RuntimeProfileUUID != qoderUUID {
			t.Fatalf("created input = %+v, want profile %s", store.createdIn, qoderUUID)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		rt, ok := body["runtime"].(map[string]any)
		if !ok || rt["provider_kind"] != agentruntime.ProviderQoderCloud.String() || rt["profile_id"] != "rprof_qoder_cn" {
			t.Fatalf("unexpected inline runtime: %#v", body["runtime"])
		}
	})

	t.Run("inherits organization default when omitted", func(t *testing.T) {
		store := newStore()
		rec := httptest.NewRecorder()
		handleCreateConsoleWorkspace(store).ServeHTTP(rec, postReq(`{"name":"Default Runtime WS"}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if store.createdIn == nil || store.createdIn.RuntimeProfileUUID != db.DefaultOMANativeRuntimeProfileUUID {
			t.Fatalf("created input = %+v", store.createdIn)
		}
	})

	t.Run("rejects disallowed profile", func(t *testing.T) {
		store := newStore()
		rec := httptest.NewRecorder()
		handleCreateConsoleWorkspace(store).ServeHTTP(rec, postReq(`{"name":"Denied WS","runtime_profile_id":"rprof_denied"}`))
		if rec.Code != http.StatusBadRequest || store.createdIn != nil {
			t.Fatalf("status = %d, created = %+v, body = %s", rec.Code, store.createdIn, rec.Body.String())
		}
	})

	t.Run("rejects inactive profile", func(t *testing.T) {
		store := newStore()
		rec := httptest.NewRecorder()
		handleCreateConsoleWorkspace(store).ServeHTTP(rec, postReq(`{"name":"Inactive WS","runtime_profile_id":"rprof_disabled"}`))
		if rec.Code != http.StatusBadRequest || store.createdIn != nil {
			t.Fatalf("status = %d, created = %+v, body = %s", rec.Code, store.createdIn, rec.Body.String())
		}
	})

	t.Run("rejects runtime modification on workspace update", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handleUpdateConsoleWorkspace(&consoleWorkspaceMutationStore{}).ServeHTTP(
			rec,
			consoleWorkspaceMutationRequest(http.MethodPatch, "wrkspc_test", `{"name":"renamed","runtime_profile_id":"rprof_qoder_cn"}`),
		)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestExternalWorkspaceOverviewAggregatesBindings(t *testing.T) {
	const workspaceUUID = "00000000-0000-4000-8000-000000000001"
	at := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	store := &runtimeProfileTestStore{
		consoleAPIKeyScopeStore: consoleAPIKeyScopeStore{
			workspaces: []ConsoleWorkspace{{
				UUID:                workspaceUUID,
				ExternalID:          "wrkspc_qoder",
				RuntimeProviderKind: agentruntime.ProviderQoderCloud.String(),
			}},
		},
		extOverview: []db.WorkspaceOverviewBucket{
			{Date: "2026-11-23", AgentID: "agent_qdr_1", AgentName: "Analyst", SessionCount: 3, FailedSessionCount: 1, LastRunAt: at.Add(-24 * time.Hour)},
			{Date: "2026-11-24", AgentID: "agent_qdr_1", AgentName: "Analyst", SessionCount: 2, FailedSessionCount: 0, LastRunAt: at},
			{Date: "2026-11-24", AgentID: "agent_qdr_2", AgentName: "Coder", SessionCount: 4, FailedSessionCount: 2, LastRunAt: at},
		},
		extAgents: []db.RuntimeResourceBinding{
			{OMAExternalID: "agent_qdr_1"},
			{OMAExternalID: "agent_qdr_2"},
			{OMAExternalID: "agent_qdr_3"},
		},
	}

	req := consoleWorkspaceAPIKeyTestRequest("wrkspc_qoder")
	rec := httptest.NewRecorder()
	handleWorkspaceOverview(store).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.extCalls != 1 || store.nativeCalls != 0 {
		t.Fatalf("extCalls=%d nativeCalls=%d, want 1 and 0", store.extCalls, store.nativeCalls)
	}
	var report workspaceOverview
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if report.AgentCount != 3 || report.SessionCount != 9 || report.FailedSessionCount != 3 || report.ActiveAgentCount != 2 || len(report.Daily) != 3 {
		t.Fatalf("unexpected overview report: %+v", report)
	}
	if report.Agents[0].AgentID != "agent_qdr_1" || report.Agents[0].SessionCount != 5 {
		t.Fatalf("unexpected top agent ranking: %+v", report.Agents)
	}
}

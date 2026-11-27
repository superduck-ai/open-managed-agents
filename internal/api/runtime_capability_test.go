package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

type stubProfileStore struct {
	byWorkspace map[string]db.RuntimeProfile
}

func (s *stubProfileStore) GetRuntimeProfileByUUID(_ context.Context, profileUUID string) (db.RuntimeProfile, error) {
	for _, profile := range s.byWorkspace {
		if profile.UUID == profileUUID {
			return profile, nil
		}
	}
	return db.RuntimeProfile{}, db.ErrNotFound
}

func (s *stubProfileStore) GetWorkspaceRuntimeProfile(_ context.Context, _, workspaceUUID string) (db.RuntimeProfile, error) {
	profile, ok := s.byWorkspace[workspaceUUID]
	if !ok {
		return db.RuntimeProfile{}, db.ErrNotFound
	}
	return profile, nil
}

func (s *stubProfileStore) UpdateRuntimeProfileHealth(context.Context, string, string, string, time.Time) error {
	return nil
}

type stubTunnelConsoleStore struct {
	workspaces []platform.ConsoleWorkspace
	adminUsers map[string]db.AdminUser
}

func (s *stubTunnelConsoleStore) ListConsoleWorkspaces(context.Context, string, bool) ([]platform.ConsoleWorkspace, error) {
	return s.workspaces, nil
}

func (s *stubTunnelConsoleStore) GetAdminUser(_ context.Context, orgUUID, userID string) (db.AdminUser, error) {
	if user, ok := s.adminUsers[userID]; ok {
		return user, nil
	}
	return db.AdminUser{UUID: "00000000-0000-4000-8000-000000000099", ExternalID: userID, OrganizationUUID: orgUUID, Role: "admin"}, nil
}

func (s *stubTunnelConsoleStore) GetAdminWorkspace(_ context.Context, orgUUID, workspaceID string) (db.AdminWorkspace, error) {
	for _, ws := range s.workspaces {
		if ws.UUID == workspaceID || ws.ExternalID == workspaceID || (workspaceID == "default" && ws.IsDefault) {
			return db.AdminWorkspace{
				UUID:                ws.UUID,
				ExternalID:          ws.ExternalID,
				OrganizationUUID:    orgUUID,
				IsDefault:           ws.IsDefault,
				RuntimeProfileUUID:  ws.RuntimeProfileUUID,
				RuntimeProviderKind: ws.RuntimeProviderKind,
			}, nil
		}
	}
	return db.AdminWorkspace{}, db.ErrNotFound
}

func (s *stubTunnelConsoleStore) GetAdminWorkspaceMember(context.Context, string, string, string) (db.AdminWorkspaceMember, error) {
	return db.AdminWorkspaceMember{}, db.ErrNotFound
}

func TestResolveTunnelConsoleScopeBlocksCrossWorkspaceBypass(t *testing.T) {
	const (
		orgUUID      = "00000000-0000-4000-8000-000000000010"
		nativeWsUUID = "00000000-0000-4000-8000-000000000011"
		qoderWsUUID  = "00000000-0000-4000-8000-000000000012"
		qoderProfID  = "00000000-0000-4000-8000-000000000022"
	)

	store := &stubTunnelConsoleStore{
		workspaces: []platform.ConsoleWorkspace{
			{
				UUID:                nativeWsUUID,
				ExternalID:          "wrkspc_native",
				OrgUUID:             orgUUID,
				IsDefault:           true,
				RuntimeProfileUUID:  db.DefaultOMANativeRuntimeProfileUUID,
				RuntimeProviderKind: agentruntime.ProviderOMANative.String(),
			},
			{
				UUID:                qoderWsUUID,
				ExternalID:          "wrkspc_qoder",
				OrgUUID:             orgUUID,
				RuntimeProfileUUID:  qoderProfID,
				RuntimeProviderKind: agentruntime.ProviderQoderCloud.String(),
			},
		},
	}

	profiles := &stubProfileStore{
		byWorkspace: map[string]db.RuntimeProfile{
			nativeWsUUID: {
				UUID:         db.DefaultOMANativeRuntimeProfileUUID,
				ExternalID:   "rprof_oma_native_default",
				ProviderKind: agentruntime.ProviderOMANative.String(),
				Status:       "active",
			},
			qoderWsUUID: {
				UUID:         qoderProfID,
				ExternalID:   "rprof_qoder_shared",
				ProviderKind: agentruntime.ProviderQoderCloud.String(),
				Status:       "active",
			},
		},
	}

	server := &Server{
		workspaceAccess:   workspaceaccess.New(store),
		consoleScopeStore: store,
		runtimeResolver:   agentruntime.NewResolver(profiles, agentruntime.NewRegistry(), nil, nil),
	}

	newConsoleRequest := func(targetWorkspaceID string, headerWorkspaceID string, principalWorkspaceUUID string, principalProvider string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/console/organizations/"+orgUUID+"/workspaces/"+targetWorkspaceID+"/mcp_tunnels", nil)
		if headerWorkspaceID != "" {
			req.Header.Set("X-Workspace-ID", headerWorkspaceID)
		}
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("orgUuid", orgUUID)
		routeCtx.URLParams.Add("workspaceId", targetWorkspaceID)
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
		ctx = auth.WithPrincipal(ctx, auth.Principal{
			OrganizationUUID:   orgUUID,
			WorkspaceUUID:      principalWorkspaceUUID,
			RuntimeProvider:    principalProvider,
			UserExternalID:     "user_admin",
			WorkspaceAccess:    auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin"},
			CredentialType:     auth.CredentialTypePlatformSession,
			RuntimeProfileUUID: db.DefaultOMANativeRuntimeProfileUUID,
		})
		return req.WithContext(ctx)
	}

	t.Run("rejects qoder target when X-Workspace-ID is omitted", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := newConsoleRequest("wrkspc_qoder", "", nativeWsUUID, agentruntime.ProviderOMANative.String())
		_, ok := server.resolveTunnelConsoleScope(rec, req)
		if ok {
			t.Fatalf("expected capability check to reject qoder workspace")
		}
		assertUnsupportedCapabilityError(t, rec)
	})

	t.Run("rejects qoder target when X-Workspace-ID spoofs native workspace", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := newConsoleRequest("wrkspc_qoder", "wrkspc_native", nativeWsUUID, agentruntime.ProviderOMANative.String())
		_, ok := server.resolveTunnelConsoleScope(rec, req)
		if ok {
			t.Fatalf("expected capability check to reject qoder workspace even with native X-Workspace-ID")
		}
		assertUnsupportedCapabilityError(t, rec)
	})

	t.Run("allows native target workspace", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := newConsoleRequest("wrkspc_native", "wrkspc_native", nativeWsUUID, agentruntime.ProviderOMANative.String())
		scope, ok := server.resolveTunnelConsoleScope(rec, req)
		if !ok {
			t.Fatalf("expected native workspace to pass, got status=%d body=%s", rec.Code, rec.Body.String())
		}
		if scope.WorkspaceUUID != nativeWsUUID {
			t.Fatalf("workspace_uuid = %q, want %q", scope.WorkspaceUUID, nativeWsUUID)
		}
	})
}

func TestRequireCapabilityMiddleware(t *testing.T) {
	const (
		orgUUID      = "00000000-0000-4000-8000-000000000010"
		nativeWsUUID = "00000000-0000-4000-8000-000000000011"
		qoderWsUUID  = "00000000-0000-4000-8000-000000000012"
	)

	profiles := &stubProfileStore{
		byWorkspace: map[string]db.RuntimeProfile{
			nativeWsUUID: {
				UUID:         db.DefaultOMANativeRuntimeProfileUUID,
				ProviderKind: agentruntime.ProviderOMANative.String(),
				Status:       "active",
			},
			qoderWsUUID: {
				UUID:         "00000000-0000-4000-8000-000000000022",
				ProviderKind: agentruntime.ProviderQoderCloud.String(),
				Status:       "active",
			},
		},
	}

	server := &Server{
		runtimeResolver: agentruntime.NewResolver(profiles, agentruntime.NewRegistry(), nil, nil),
	}

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, capKey := range []string{
		agentruntime.CapabilityMCPTunnels,
		agentruntime.CapabilitySessionWebhooks,
		agentruntime.CapabilityObservabilityDashboard,
		agentruntime.CapabilityMCPServers,
		agentruntime.CapabilityEvaluations,
	} {
		t.Run("blocks qoder on "+capKey, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
				OrganizationUUID: orgUUID,
				WorkspaceUUID:    qoderWsUUID,
				RuntimeProvider:  agentruntime.ProviderQoderCloud.String(),
			}))
			server.requireCapability(capKey)(okHandler).ServeHTTP(rec, req)
			assertUnsupportedCapabilityError(t, rec)
		})

		t.Run("allows native on "+capKey, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
				OrganizationUUID: orgUUID,
				WorkspaceUUID:    nativeWsUUID,
				RuntimeProvider:  agentruntime.ProviderOMANative.String(),
			}))
			server.requireCapability(capKey)(okHandler).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
		})
	}

	t.Run("allows qoder on credential_vaults", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/vaults", nil)
		req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
			OrganizationUUID: orgUUID,
			WorkspaceUUID:    qoderWsUUID,
			RuntimeProvider:  agentruntime.ProviderQoderCloud.String(),
		}))
		server.requireCapability(agentruntime.CapabilityCredentialVaults)(okHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("allows qoder on skills", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/skills", nil)
		req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
			OrganizationUUID: orgUUID,
			WorkspaceUUID:    qoderWsUUID,
			RuntimeProvider:  agentruntime.ProviderQoderCloud.String(),
		}))
		server.requireCapability(agentruntime.CapabilitySkills)(okHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("allows qoder on deployments", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/deployments", nil)
		req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
			OrganizationUUID: orgUUID,
			WorkspaceUUID:    qoderWsUUID,
			RuntimeProvider:  agentruntime.ProviderQoderCloud.String(),
		}))
		server.requireCapability(agentruntime.CapabilityDeployments)(okHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("allows qoder on memory_stores", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/memory_stores", nil)
		req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
			OrganizationUUID: orgUUID,
			WorkspaceUUID:    qoderWsUUID,
			RuntimeProvider:  agentruntime.ProviderQoderCloud.String(),
		}))
		server.requireCapability(agentruntime.CapabilityMemoryStores)(okHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestRequireConsoleWorkspaceCapabilityGuardsMCPServersAndEvaluations(t *testing.T) {
	const (
		orgUUID      = "00000000-0000-4000-8000-000000000010"
		nativeWsUUID = "00000000-0000-4000-8000-000000000011"
		qoderWsUUID  = "00000000-0000-4000-8000-000000000012"
		qoderProfID  = "00000000-0000-4000-8000-000000000022"
	)

	store := &stubTunnelConsoleStore{
		workspaces: []platform.ConsoleWorkspace{
			{
				UUID:                nativeWsUUID,
				ExternalID:          "wrkspc_native",
				OrgUUID:             orgUUID,
				IsDefault:           true,
				RuntimeProfileUUID:  db.DefaultOMANativeRuntimeProfileUUID,
				RuntimeProviderKind: agentruntime.ProviderOMANative.String(),
			},
			{
				UUID:                qoderWsUUID,
				ExternalID:          "wrkspc_qoder",
				OrgUUID:             orgUUID,
				RuntimeProfileUUID:  qoderProfID,
				RuntimeProviderKind: agentruntime.ProviderQoderCloud.String(),
			},
		},
	}

	profiles := &stubProfileStore{
		byWorkspace: map[string]db.RuntimeProfile{
			nativeWsUUID: {
				UUID:         db.DefaultOMANativeRuntimeProfileUUID,
				ProviderKind: agentruntime.ProviderOMANative.String(),
				Status:       "active",
			},
			qoderWsUUID: {
				UUID:         qoderProfID,
				ProviderKind: agentruntime.ProviderQoderCloud.String(),
				Status:       "active",
			},
		},
	}

	server := &Server{
		workspaceAccess:   workspaceaccess.New(store),
		consoleScopeStore: store,
		runtimeResolver:   agentruntime.NewResolver(profiles, agentruntime.NewRegistry(), nil, nil),
	}

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	router := chi.NewRouter()
	router.Route("/api/console/organizations/{orgUuid}", func(r chi.Router) {
		r.Group(func(g chi.Router) {
			g.Use(server.requireConsoleWorkspaceCapability(agentruntime.CapabilityMCPServers))
			g.Get("/workspaces/{workspaceId}/mcp_servers", okHandler)
		})
		r.Group(func(g chi.Router) {
			g.Use(server.requireConsoleWorkspaceCapability(agentruntime.CapabilityEvaluations))
			g.Get("/workspaces/{workspaceId}/evaluations/datasets", okHandler)
		})
	})

	send := func(path string, headerWs string, principalWs string, principalProvider string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if headerWs != "" {
			req.Header.Set("X-Workspace-ID", headerWs)
		}
		req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
			OrganizationUUID:   orgUUID,
			WorkspaceUUID:      principalWs,
			RuntimeProvider:    principalProvider,
			UserExternalID:     "user_admin",
			WorkspaceAccess:    auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin"},
			CredentialType:     auth.CredentialTypePlatformSession,
			RuntimeProfileUUID: db.DefaultOMANativeRuntimeProfileUUID,
		}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	for _, suffix := range []string{"/mcp_servers", "/evaluations/datasets"} {
		t.Run("blocks qoder target with spoofed native header on "+suffix, func(t *testing.T) {
			rec := send("/api/console/organizations/"+orgUUID+"/workspaces/wrkspc_qoder"+suffix, "wrkspc_native", nativeWsUUID, agentruntime.ProviderOMANative.String())
			assertUnsupportedCapabilityError(t, rec)
		})
		t.Run("blocks qoder target without header on "+suffix, func(t *testing.T) {
			rec := send("/api/console/organizations/"+orgUUID+"/workspaces/wrkspc_qoder"+suffix, "", nativeWsUUID, agentruntime.ProviderOMANative.String())
			assertUnsupportedCapabilityError(t, rec)
		})
		t.Run("allows native target on "+suffix, func(t *testing.T) {
			rec := send("/api/console/organizations/"+orgUUID+"/workspaces/wrkspc_native"+suffix, "wrkspc_native", nativeWsUUID, agentruntime.ProviderOMANative.String())
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestApplyWorkspaceRuntimeFallback(t *testing.T) {
	var principal auth.Principal
	applyWorkspaceRuntime(&principal, db.AdminWorkspace{})
	if principal.RuntimeProvider != agentruntime.ProviderOMANative.String() {
		t.Fatalf("RuntimeProvider = %q, want %q", principal.RuntimeProvider, agentruntime.ProviderOMANative.String())
	}
	if principal.RuntimeProfileUUID != db.DefaultOMANativeRuntimeProfileUUID {
		t.Fatalf("RuntimeProfileUUID = %q, want %q", principal.RuntimeProfileUUID, db.DefaultOMANativeRuntimeProfileUUID)
	}

	applyWorkspaceRuntime(&principal, db.AdminWorkspace{
		RuntimeProviderKind: agentruntime.ProviderQoderCloud.String(),
		RuntimeProfileUUID:  "00000000-0000-4000-8000-000000000099",
	})
	if principal.RuntimeProvider != agentruntime.ProviderQoderCloud.String() || principal.RuntimeProfileUUID != "00000000-0000-4000-8000-000000000099" {
		t.Fatalf("unexpected principal runtime: %+v", principal)
	}
}

func assertUnsupportedCapabilityError(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal error body %s: %v", rec.Body.String(), err)
	}
	if payload.Error.Type != "unsupported_capability_error" {
		t.Fatalf("error.type = %q, want unsupported_capability_error", payload.Error.Type)
	}
}

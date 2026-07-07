package api

import (
	"errors"
	"log"
	"log/slog"
	"net/http"
	"strings"

	adminapi "github.com/superduck-ai/open-managed-agents/internal/admin"
	"github.com/superduck-ai/open-managed-agents/internal/agents"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/batches"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	deploymentsapi "github.com/superduck-ai/open-managed-agents/internal/deployments"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/files"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	memoryapi "github.com/superduck-ai/open-managed-agents/internal/memory"
	modelsapi "github.com/superduck-ai/open-managed-agents/internal/models"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	platformapi "github.com/superduck-ai/open-managed-agents/internal/platformapi"
	"github.com/superduck-ai/open-managed-agents/internal/platformauth"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"
	webhooksapi "github.com/superduck-ai/open-managed-agents/internal/webhooks"
	workbenchapi "github.com/superduck-ai/open-managed-agents/internal/workbench"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	cfg            config.Config
	db             *db.DB
	router         chi.Router
	platformStore  platformsession.Store
	admin          *adminapi.Handler
	agents         *agents.Handler
	batch          *batches.Handler
	codeSessions   *codesessions.Service
	deployments    *deploymentsapi.Handler
	deploymentRuns *deploymentsapi.RunsHandler
	envs           *environments.Handler
	files          *files.Handler
	memory         *memoryapi.Handler
	models         *modelsapi.Handler
	sessions       *sessionsapi.Handler
	skills         *skillsapi.Handler
	vaults         *vaultsapi.Handler
	webhooks       *webhooksapi.Handler
}

type apiEntrypointRouter struct {
	service  http.Handler
	platform http.Handler
}

func NewServer(cfg config.Config, database *db.DB, objectStore storage.ObjectStore) *Server {
	return NewServerWithLogger(cfg, database, objectStore, nil)
}

func NewServerWithLogger(cfg config.Config, database *db.DB, objectStore storage.ObjectStore, logger *slog.Logger) *Server {
	return NewServerWithPlatformSessions(cfg, database, objectStore, logger, platformsession.NewMemoryStore())
}

func NewServerWithPlatformSessions(cfg config.Config, database *db.DB, objectStore storage.ObjectStore, logger *slog.Logger, platformStore platformsession.Store) *Server {
	if platformStore == nil {
		platformStore = platformsession.NewMemoryStore()
	}
	codeSessionService := codesessions.NewService(cfg, database)
	s := &Server{
		cfg:            cfg,
		db:             database,
		platformStore:  platformStore,
		admin:          adminapi.NewHandler(cfg, database),
		agents:         agents.NewHandler(cfg, database),
		batch:          batches.NewHandler(cfg, database, objectStore),
		codeSessions:   codeSessionService,
		deployments:    deploymentsapi.NewHandler(cfg, database),
		deploymentRuns: deploymentsapi.NewRunsHandler(cfg, database),
		envs:           environments.NewHandler(cfg, database),
		files:          files.NewHandler(cfg, database, objectStore),
		memory:         memoryapi.NewHandler(cfg, database, objectStore),
		models:         modelsapi.NewHandler(),
		sessions:       sessionsapi.NewHandler(cfg, database, codeSessionService),
		skills:         skillsapi.NewHandler(cfg, database, objectStore),
		vaults:         vaultsapi.NewHandler(cfg, database),
		webhooks:       webhooksapi.NewHandler(cfg, database),
	}
	codeSessionService.SetBridgeAuthenticator(s.authenticateCodeSessionBridge)

	router := chi.NewRouter()
	router.Use(s.requestIDMiddleware)
	if logger != nil {
		router.Use(requestLoggingMiddleware(logger.With("component", "http")))
	}
	router.Use(s.recoverMiddleware)
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	codeSessionService.RegisterRoutes(router)
	v1Entrypoints := s.v1EntrypointRouter()
	router.Handle("/v1", v1Entrypoints)
	router.Handle("/v1/*", v1Entrypoints)
	platformConsoleAPI := s.platformConsoleAPIRouter()
	router.Handle("/api", platformConsoleAPI)
	router.Handle("/api/*", platformConsoleAPI)
	router.Handle("/auth", platformConsoleAPI)
	router.Handle("/auth/*", platformConsoleAPI)
	router.Handle("/oauth", platformConsoleAPI)
	router.Handle("/oauth/*", platformConsoleAPI)
	router.Handle("/web-api", platformConsoleAPI)
	router.Handle("/web-api/*", platformConsoleAPI)
	s.router = router
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (s *Server) v1EntrypointRouter() http.Handler {
	return apiEntrypointRouter{
		service:  s.serviceAPIRouter(),
		platform: s.platformAPIRouter(),
	}
}

func (r apiEntrypointRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Route by authentication credential.
	if auth.ExtractAPIKey(req) != "" {
		r.service.ServeHTTP(w, req)
		return
	}
	if auth.ExtractPlatformSessionKey(req) != "" {
		r.platform.ServeHTTP(w, req)
		return
	}
	r.platform.ServeHTTP(w, req)
}

func (s *Server) serviceAPIRouter() chi.Router {
	router := chi.NewRouter()
	router.Route("/v1", func(r chi.Router) {
		r.Use(s.serviceAuthMiddleware)
		r.NotFound(notFound)
		r.MethodNotAllowed(notFound)
		s.mountServiceV1Resources(r)
	})
	return router
}

func (s *Server) platformAPIRouter() chi.Router {
	router := chi.NewRouter()
	router.Route("/v1", func(r chi.Router) {
		r.NotFound(notFound)
		r.MethodNotAllowed(notFound)
		platformapi.RegisterPlatformPrivacyConsentRoutes(r)
		r.Group(func(r chi.Router) {
			r.Use(s.platformAuthMiddleware)
			s.mountPlatformV1Resources(r)
		})
	})
	return router
}

func (s *Server) platformConsoleAPIRouter() chi.Router {
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Group(func(r chi.Router) {
		r.Use(s.optionalPlatformAuthMiddleware)
		platformapi.RegisterDirectoryRoutes(r)
		platformapi.RegisterPlatformAccountRoutes(r, s.db)
		platformapi.RegisterPlatformEmailLoginRoutes(r, s.db, platformauth.New(s.db), s.platformStore)
		platformapi.RegisterPlatformBillingRoutes(r)
	})
	router.Get("/oauth/vault/success", s.handlePlatformMCPVaultAuthCallback)
	router.Group(func(r chi.Router) {
		r.Use(s.platformAuthMiddleware)
		r.Route("/api/organizations/{orgUuid}", func(r chi.Router) {
			platformapi.RegisterOrganizationRootRoutes(r, s.db)
			platformapi.RegisterOrganizationProfileRoutes(r, s.db)
			platformapi.RegisterOrganizationSSORoutes(r)
			platformapi.RegisterOrganizationOnboardingRoutes(r)
			platformapi.RegisterOrganizationExperienceRoutes(r)
			platformapi.RegisterOrganizationBillingRoutes(r)
			platformapi.RegisterOrganizationAnalyticsRoutes(r)
			platformapi.RegisterOrganizationProxyRoutes(r, s.cfg)
			workbenchapi.RegisterOrgWorkbenchRoutes(r, s.db)
			r.Post("/mcp/vault-auth/start", s.handlePlatformMCPVaultAuthStart)
		})
		r.Route("/api/oauth/organizations/{orgUuid}", func(r chi.Router) {
			platformapi.RegisterOrganizationOAuthEnvironmentRoutes(r)
		})
		r.Route("/api/console/organizations/{orgUuid}", func(r chi.Router) {
			platformapi.RegisterConsoleOrganizationWorkspaceRoutes(r, s.db)
			platformapi.RegisterConsoleOrganizationAdminRequestRoutes(r, s.db)
			platformapi.RegisterConsoleOrganizationAPIKeyRoutes(r, s.db)
			platformapi.RegisterConsoleOrganizationMemberRoutes(r, s.db)
			platformapi.RegisterConsoleOrganizationInviteRoutes(r, s.db)
		})
		r.Route("/api/{orgUuid}", func(r chi.Router) {
			s.files.RegisterPlatformRoutes(r)
		})
		r.Get("/web-api/sessions/{sessionId}/stream", s.handlePlatformWebSessionStream)
	})
	return router
}

func (s *Server) mountServiceV1Resources(r chi.Router) {
	s.mountSharedV1Resources(r)
}

func (s *Server) mountPlatformV1Resources(r chi.Router) {
	s.mountSharedV1Resources(r)
}

func (s *Server) mountSharedV1Resources(r chi.Router) {
	r.Post("/agents:search", s.agents.Search)
	r.Mount("/agents", s.agents)
	r.Mount("/deployment_runs", s.deploymentRuns)
	r.Mount("/deployments", s.deployments)
	r.Mount("/environments", s.envs)
	r.Mount("/files", s.files)
	r.Mount("/memory_stores", s.memory)
	r.Mount("/messages/batches", s.batch)
	r.Mount("/models", s.models)
	r.Mount("/organizations", s.admin)
	r.Mount("/sessions", s.sessions)
	r.Mount("/skills", s.skills)
	r.Mount("/vaults", s.vaults)
	r.Mount("/webhooks", s.webhooks)
}

func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("request-id")
		if requestID == "" {
			generated, err := ids.New("req_")
			if err != nil {
				requestID = "req_unknown"
			} else {
				requestID = generated
			}
		}
		w.Header().Set("request-id", requestID)
		next.ServeHTTP(w, r.WithContext(httpapi.WithRequestID(r.Context(), requestID)))
	})
}

func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				requestID := httpapi.RequestID(r.Context())
				log.Printf("panic request_id=%s: %v", requestID, recovered)
				httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) serviceAuthMiddleware(next http.Handler) http.Handler {
	return s.authenticated(next, s.authenticateService)
}

func (s *Server) platformAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := s.authenticatePlatformSession(r)
		if err != nil {
			if recovered, recoveredOrgAlias, recoveredErr, ok := s.recoverPlatformMirrorSession(r); ok {
				if recoveredErr == nil {
					setPlatformRecoveredSessionCookies(w, recovered)
					ctx := auth.WithPrincipal(r.Context(), recovered)
					if recoveredOrgAlias != "" {
						ctx = auth.WithPlatformMirrorOrganizationAlias(ctx, recoveredOrgAlias)
					}
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				if recoveredErr.Status >= http.StatusInternalServerError {
					httpapi.WriteError(w, r, recoveredErr)
					return
				}
			}
			if auth.ExtractPlatformSessionKey(r) != "" {
				clearPlatformSessionCookies(w)
			}
			httpapi.WriteError(w, r, err)
			return
		}
		ctx := auth.WithPrincipal(r.Context(), principal)
		if orgAlias := s.platformMirrorOrganizationAlias(r, principal); orgAlias != "" {
			ctx = auth.WithPlatformMirrorOrganizationAlias(ctx, orgAlias)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) optionalPlatformAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.ExtractPlatformSessionKey(r) == "" {
			next.ServeHTTP(w, r)
			return
		}
		principal, err := s.authenticatePlatformSession(r)
		if err != nil {
			clearPlatformSessionCookies(w)
			if err.Status >= http.StatusInternalServerError {
				httpapi.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func (s *Server) authenticated(next http.Handler, authenticate func(*http.Request) (auth.Principal, *httpapi.Error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticate(r)
		if err != nil {
			if auth.ExtractPlatformSessionKey(r) != "" {
				clearPlatformSessionCookies(w)
			}
			httpapi.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func clearPlatformSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{"sessionKey", "lastActiveOrg"} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: false,
		})
	}
}

func setPlatformRecoveredSessionCookies(w http.ResponseWriter, principal auth.Principal) {
	orgUUID := strings.TrimSpace(principal.OrganizationUUID)
	if orgUUID == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "lastActiveOrg",
		Value:  orgUUID,
		Path:   "/",
		MaxAge: int(platformsession.DefaultTTL.Seconds()),
	})
}

func (s *Server) authenticateService(r *http.Request) (auth.Principal, *httpapi.Error) {
	apiKey := auth.ExtractAPIKey(r)
	if apiKey == "" {
		return auth.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key")
	}
	key, err := s.db.GetAPIKey(r.Context(), auth.HashAPIKey(apiKey))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			if isEnvironmentCredentialPath(r.URL.Path) {
				envKey, envErr := s.db.GetEnvironmentKey(r.Context(), auth.HashAPIKey(apiKey))
				if envErr == nil {
					return auth.Principal{
						CredentialType:         auth.CredentialTypeEnvironmentKey,
						EnvironmentKeyID:       envKey.ID,
						OrganizationID:         envKey.OrganizationID,
						OrganizationExternalID: envKey.OrganizationExternalID,
						WorkspaceID:            envKey.WorkspaceID,
						WorkspaceUUID:          envKey.WorkspaceUUID,
						WorkspaceExternalID:    envKey.WorkspaceExternalID,
						EnvironmentID:          envKey.EnvironmentID,
						EnvironmentExternalID:  envKey.EnvironmentExternalID,
					}, nil
				}
				if envErr != nil && !errors.Is(envErr, db.ErrNotFound) {
					log.Printf("authenticate environment key: %v", envErr)
					return auth.Principal{}, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
				}
			}
			return auth.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid API key")
		}
		log.Printf("authenticate api key: %v", err)
		return auth.Principal{}, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	return auth.Principal{
		CredentialType:         auth.CredentialTypeAPIKey,
		APIKeyID:               key.ID,
		APIKeyExternalID:       key.ExternalID,
		OrganizationID:         key.OrganizationID,
		OrganizationExternalID: key.OrganizationExternalID,
		WorkspaceID:            key.WorkspaceID,
		WorkspaceUUID:          key.WorkspaceUUID,
		WorkspaceExternalID:    key.WorkspaceExternalID,
	}, nil
}

func (s *Server) authenticateCodeSessionBridge(r *http.Request, codeSessionID string) (auth.Principal, *httpapi.Error) {
	principal, apiErr := s.authenticateService(r)
	if apiErr != nil {
		return auth.Principal{}, apiErr
	}
	codeSessionID = strings.TrimSpace(codeSessionID)
	if codeSessionID == "" {
		return auth.Principal{}, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found")
	}
	record, err := s.db.GetCodeSession(r.Context(), codeSessionID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return auth.Principal{}, httpapi.NewError(http.StatusNotFound, "not_found_error", "Code session not found")
		}
		log.Printf("authenticate code session bridge: %v", err)
		return auth.Principal{}, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	if record.OrganizationID != principal.OrganizationID || record.WorkspaceID != principal.WorkspaceID {
		return auth.Principal{}, httpapi.NewError(http.StatusNotFound, "not_found_error", "Code session not found")
	}
	return principal, nil
}

func (s *Server) authenticatePlatformSession(r *http.Request) (auth.Principal, *httpapi.Error) {
	sessionKey := auth.ExtractPlatformSessionKey(r)
	if sessionKey == "" {
		return auth.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing sessionKey cookie")
	}
	session, err := s.platformStore.Get(r.Context(), sessionKey)
	if err != nil {
		if errors.Is(err, platformsession.ErrNotFound) {
			return auth.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid session")
		}
		log.Printf("authenticate platform session: %v", err)
		return auth.Principal{}, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	if strings.TrimSpace(session.OrganizationUUID) == "" && strings.TrimSpace(session.OrganizationExternalID) != "" {
		if org, orgErr := s.db.GetPlatformOrganization(r.Context(), session.OrganizationExternalID); orgErr == nil && org != nil {
			session.OrganizationUUID = org.UUID
		}
	}
	principal := session.Principal()
	principal, orgErr := s.applyPlatformOrganizationOverride(r, principal)
	if orgErr != nil {
		return auth.Principal{}, orgErr
	}
	workspaceID := platformWorkspaceOverrideID(r)
	if workspaceID == "" || workspaceID == "default" || workspaceID == principal.WorkspaceExternalID || workspaceID == principal.WorkspaceUUID {
		return principal, nil
	}
	workspace, err := s.db.GetAdminWorkspace(r.Context(), principal.OrganizationID, workspaceID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return auth.Principal{}, httpapi.NewError(http.StatusForbidden, "permission_error", "Workspace not found")
		}
		log.Printf("load platform workspace override: %v", err)
		return auth.Principal{}, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	if workspace.ArchivedAt != nil {
		return auth.Principal{}, httpapi.NewError(http.StatusForbidden, "permission_error", "Workspace is archived")
	}
	principal.WorkspaceID = workspace.ID
	principal.WorkspaceUUID = workspace.UUID
	principal.WorkspaceExternalID = workspace.ExternalID
	return principal, nil
}

func (s *Server) recoverPlatformMirrorSession(r *http.Request) (auth.Principal, string, *httpapi.Error, bool) {
	sessionKey := auth.ExtractPlatformSessionKey(r)
	if sessionKey == "" || s.db == nil || s.platformStore == nil {
		return auth.Principal{}, "", nil, false
	}
	preferredOrgID := platformSessionRecoveryOrgID(r)
	if preferredOrgID == "" && isPlatformAPIRequestPath(r.URL.Path) {
		return auth.Principal{}, "", nil, false
	}

	recoveredOrgAlias := ""
	userExternalID, orgUUID, err := s.db.FindBootstrapUserContext(r.Context(), preferredOrgID)
	if err != nil {
		if preferredOrgID != "" {
			userExternalID, orgUUID, err = s.db.FindBootstrapUserContext(r.Context(), "")
			if err == nil {
				recoveredOrgAlias = preferredOrgID
			}
		}
		if err != nil {
			if errors.Is(err, db.ErrNotFound) || errors.Is(err, platform.ErrNotFound) {
				return auth.Principal{}, "", httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid session"), true
			}
			log.Printf("recover platform session context: %v", err)
			return auth.Principal{}, "", httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed"), true
		}
	}
	session, err := s.db.ResolvePlatformSessionIdentity(r.Context(), platformsession.CreateInput{
		SessionKey: sessionKey,
		UserUUID:   userExternalID,
		OrgUUID:    orgUUID,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) || errors.Is(err, platform.ErrNotFound) {
			return auth.Principal{}, "", httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid session"), true
		}
		log.Printf("recover platform session identity: %v", err)
		return auth.Principal{}, "", httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed"), true
	}
	if err := s.platformStore.Save(r.Context(), sessionKey, session); err != nil {
		log.Printf("save recovered platform session: %v", err)
		return auth.Principal{}, "", httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed"), true
	}
	principal := session.Principal()
	principal, orgErr := s.applyPlatformOrganizationOverride(r, principal)
	if orgErr != nil {
		return auth.Principal{}, "", orgErr, true
	}
	return principal, recoveredOrgAlias, nil, true
}

func (s *Server) applyPlatformOrganizationOverride(r *http.Request, principal auth.Principal) (auth.Principal, *httpapi.Error) {
	orgID := platformOrganizationOverrideID(r)
	if orgID == "" || orgID == principal.OrganizationUUID || orgID == principal.OrganizationExternalID {
		return principal, nil
	}
	org, err := s.db.GetPlatformOrganization(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) || errors.Is(err, platform.ErrNotFound) {
			return auth.Principal{}, httpapi.NewError(http.StatusForbidden, "permission_error", "Organization not found")
		}
		log.Printf("load platform organization override: %v", err)
		return auth.Principal{}, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	if org.UUID != principal.OrganizationUUID && org.ExternalID != principal.OrganizationExternalID {
		return auth.Principal{}, httpapi.NewError(http.StatusForbidden, "permission_error", "Organization not allowed")
	}
	principal.OrganizationUUID = org.UUID
	principal.OrganizationExternalID = org.ExternalID
	return principal, nil
}

func (s *Server) platformMirrorOrganizationAlias(r *http.Request, principal auth.Principal) string {
	if s.db == nil {
		return ""
	}
	orgID := platformAPIPathOrganizationID(r.URL.Path)
	if orgID == "" || orgID == principal.OrganizationUUID || orgID == principal.OrganizationExternalID {
		return ""
	}
	org, err := s.db.GetPlatformOrganization(r.Context(), orgID)
	if err == nil {
		if org.UUID == principal.OrganizationUUID || org.ExternalID == principal.OrganizationExternalID {
			return ""
		}
		return ""
	}
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, platform.ErrNotFound) {
		return orgID
	}
	log.Printf("load platform mirror organization alias: %v", err)
	return ""
}

func platformOrganizationOverrideID(r *http.Request) string {
	for _, value := range []string{
		r.Header.Get("X-Organization-UUID"),
		r.URL.Query().Get("organization_uuid"),
	} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func platformSessionRecoveryOrgID(r *http.Request) string {
	if segment := platformAPIPathOrganizationID(r.URL.Path); segment != "" {
		return segment
	}
	if value := platformOrganizationOverrideID(r); value != "" {
		return value
	}
	if cookie, err := r.Cookie("lastActiveOrg"); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

func platformAPIPathOrganizationID(path string) string {
	for _, prefix := range []string{
		"/api/console/organizations/",
		"/api/oauth/organizations/",
		"/api/organizations/",
	} {
		if segment := firstPathSegmentAfterPrefix(path, prefix); segment != "" {
			return segment
		}
	}
	if segment := firstPathSegmentAfterPrefix(path, "/api/"); segment != "" && platformGenericAPIOrgSegment(segment) {
		return segment
	}
	return ""
}

func firstPathSegmentAfterPrefix(path string, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	segment, _, _ := strings.Cut(rest, "/")
	return strings.TrimSpace(segment)
}

func platformGenericAPIOrgSegment(segment string) bool {
	if strings.Contains(segment, ".") || strings.Contains(segment, "_") {
		return true
	}
	return len(segment) == 36 && strings.Count(segment, "-") == 4
}

func platformWorkspaceOverrideID(r *http.Request) string {
	for _, value := range []string{
		r.Header.Get("X-Workspace-ID"),
		r.URL.Query().Get("workspace_id"),
	} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isPlatformAPIRequestPath(path string) bool {
	for _, prefix := range []string{"/api", "/v1", "/auth", "/oauth", "/web-api"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func isEnvironmentWorkPath(path string) bool {
	return strings.HasPrefix(path, "/v1/environments/") && strings.Contains(path, "/work")
}

func isEnvironmentCredentialPath(path string) bool {
	return isEnvironmentWorkPath(path) || strings.HasPrefix(path, "/v1/sessions/") || path == "/v1/skills" || strings.HasPrefix(path, "/v1/skills/")
}

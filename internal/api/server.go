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
	filestoreapi "github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/mcpcatalogs"
	memoryapi "github.com/superduck-ai/open-managed-agents/internal/memory"
	messagesapi "github.com/superduck-ai/open-managed-agents/internal/messages"
	modelsapi "github.com/superduck-ai/open-managed-agents/internal/models"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	platformapi "github.com/superduck-ai/open-managed-agents/internal/platformapi"
	"github.com/superduck-ai/open-managed-agents/internal/platformauth"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
	"github.com/superduck-ai/open-managed-agents/internal/skillprewarm"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"
	webhooksapi "github.com/superduck-ai/open-managed-agents/internal/webhooks"
	workbenchapi "github.com/superduck-ai/open-managed-agents/internal/workbench"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	cfg                  config.Config
	db                   *db.DB
	router               chi.Router
	platformStore        platformsession.Store
	filestoreCredentials *filestoreapi.TokenCredentials
	admin                *adminapi.Handler
	agents               *agents.Handler
	batch                *batches.Handler
	codeSessions         *codesessions.Handler
	deployments          *deploymentsapi.Handler
	deploymentRuns       *deploymentsapi.RunsHandler
	envs                 *environments.Handler
	files                *files.Handler
	filestore            *filestoreapi.Handler
	memory               *memoryapi.Handler
	messages             *messagesapi.Handler
	models               *modelsapi.Handler
	sessions             *sessionsapi.Handler
	skills               *skillsapi.Handler
	vaults               *vaultsapi.Handler
	webhooks             *webhooksapi.Handler
}

// ServerDeps 汇总组装 HTTP API Server 所需依赖。
// PlatformStore 为 nil 时回落到内存 store。
// ObjectStore 由应用启动层从共享 storage.Client 派生，绑定默认 bucket，供对象资源与 Filestore 共用。
type ServerDeps struct {
	Config                 config.Config
	DB                     *db.DB
	ObjectStore            storage.ObjectStore
	Logger                 *slog.Logger
	PlatformStore          platformsession.Store
	CodeSessionCredentials *codesessions.SessionCredentials
	FilestoreCredentials   *filestoreapi.TokenCredentials
}

// NewServer 用显式依赖组装 HTTP API Server。
// 注入 CodeSessionCredentials，保证 HTTP 验签与 sandbox 启动签发使用同一公钥身份。
func NewServer(deps ServerDeps) *Server {
	platformStore := deps.PlatformStore
	if platformStore == nil {
		platformStore = platformsession.NewMemoryStore()
	}
	codeSessionService := codesessions.NewServiceWithCredentials(deps.DB, deps.CodeSessionCredentials)
	skillPrewarmEnqueuer := skillprewarm.NewEnqueuer(deps.DB)
	filestoreHandler := filestoreapi.NewHandler(
		deps.Config,
		filestoreapi.NewService(deps.Config, deps.DB, deps.ObjectStore),
	)
	s := &Server{
		cfg:                  deps.Config,
		db:                   deps.DB,
		platformStore:        platformStore,
		filestoreCredentials: deps.FilestoreCredentials,
		admin:                adminapi.NewHandler(deps.Config, deps.DB),
		agents:               agents.NewHandlerWithSkillPrewarm(deps.Config, deps.DB, skillPrewarmEnqueuer),
		batch:                batches.NewHandler(deps.Config, deps.DB, deps.ObjectStore),
		codeSessions:         codesessions.NewHandler(deps.Config, codeSessionService),
		deployments:          deploymentsapi.NewHandlerWithSkillPrewarm(deps.Config, deps.DB, skillPrewarmEnqueuer),
		deploymentRuns:       deploymentsapi.NewRunsHandler(deps.Config, deps.DB),
		envs:                 environments.NewHandler(deps.Config, deps.DB),
		files:                files.NewHandler(deps.Config, deps.DB, deps.ObjectStore),
		filestore:            filestoreHandler,
		memory:               memoryapi.NewHandler(deps.Config, deps.DB, deps.ObjectStore),
		messages:             messagesapi.NewHandler(deps.Config),
		models:               modelsapi.NewHandler(),
		sessions:             sessionsapi.NewHandler(deps.Config, deps.DB, codeSessionService),
		skills:               skillsapi.NewHandlerWithSkillPrewarm(deps.Config, deps.DB, deps.ObjectStore, skillPrewarmEnqueuer),
		vaults:               vaultsapi.NewHandler(deps.Config, deps.DB),
		webhooks:             webhooksapi.NewHandler(deps.Config.Webhook, deps.DB),
	}
	router := chi.NewRouter()
	router.Use(s.requestIDMiddleware)
	if deps.Logger != nil {
		router.Use(requestLoggingMiddleware(deps.Logger.With("component", "http")))
	}
	router.Use(s.recoverMiddleware)
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.registerVersionedAPIRoutes(router)
	s.registerPlatformConsoleRoutes(router)
	s.router = router
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (s *Server) registerVersionedAPIRoutes(router chi.Router) {
	router.Route("/v1", func(r chi.Router) {
		// code-session runtime、worker 与旧版 ingress 各自执行协议鉴权，因此注册在 workspace/service 通用鉴权组之外。
		s.codeSessions.RegisterV1Routes(r)
		platformapi.RegisterPlatformPrivacyConsentRoutes(r)
		// 整个 Filestore 命名空间使用专用鉴权和错误外观；具体协议操作由资源 Handler 校验。
		r.Route("/filestore", func(r chi.Router) {
			r.Use(s.filestoreAuthMiddleware)
			r.Mount("/fs", s.filestore)
			r.NotFound(filestoreNotFound)
			r.MethodNotAllowed(filestoreNotFound)
		})
		r.With(s.v1AuthMiddleware).Group(func(r chi.Router) {
			s.registerAuthenticatedV1Routes(r)
		})
		// 未知路径和错误 method 的 fallback 也先鉴权，保持统一的 API 级鉴权边界和错误语义。
		r.With(s.v1AuthMiddleware).NotFound(notFound)
		r.With(s.v1AuthMiddleware).MethodNotAllowed(notFound)
	})
	router.Route("/v2", s.codeSessions.RegisterV2Routes)
}

func (s *Server) filestoreAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, apiErr := s.authenticateFilestore(r)
		if apiErr != nil {
			code := "unauthenticated"
			message := "Invalid bearer token"
			if apiErr.Status >= http.StatusInternalServerError {
				code = "internal"
				message = "Authentication failed"
			}
			// 鉴权发生在 Handler 之外，也必须维持 rclone-filestore 可识别的扁平错误信封。
			filestoreapi.WriteProtocolError(w, apiErr.Status, code, message)
			return
		}
		next.ServeHTTP(w, r.WithContext(filestoreapi.WithPrincipal(r.Context(), principal)))
	})
}

func filestoreNotFound(w http.ResponseWriter, _ *http.Request) {
	filestoreapi.WriteProtocolError(w, http.StatusNotFound, "not_found", "Not found")
}

func (s *Server) registerPlatformConsoleRoutes(router chi.Router) {
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
			workbenchapi.RegisterOrgWorkbenchRoutes(r, s.db, s.cfg.AnthropicUpstream)
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
			mcpcatalogs.NewHandler(s.db).RegisterRoutes(r)
		})
		r.Route("/api/{orgUuid}", func(r chi.Router) {
			s.files.RegisterPlatformRoutes(r)
		})
		r.Get("/web-api/sessions/{sessionId}/stream", s.handlePlatformWebSessionStream)
	})
}

func (s *Server) registerAuthenticatedV1Routes(r chi.Router) {
	r.Post("/agents:search", s.agents.Search)
	r.Mount("/agents", s.agents)
	r.Mount("/deployment_runs", s.deploymentRuns)
	r.Mount("/deployments", s.deployments)
	r.Mount("/environments", s.envs)
	r.Mount("/files", s.files)
	r.Mount("/memory_stores", s.memory)
	r.Post("/messages", s.messages.Create)
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

// v1AuthMiddleware 优先把携带 API key 的请求送入 service 鉴权，否则使用 platform session，使资源路由不必注册两次。
func (s *Server) v1AuthMiddleware(next http.Handler) http.Handler {
	service := s.serviceAuthMiddleware(next)
	platform := s.platformAuthMiddleware(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if usesServiceAuthentication(r) {
			service.ServeHTTP(w, r)
			return
		}
		platform.ServeHTTP(w, r)
	})
}

func usesServiceAuthentication(r *http.Request) bool {
	return auth.ExtractAPIKey(r) != ""
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

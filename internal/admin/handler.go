package admin

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
	router  chi.Router
}

func NewHandler(cfg config.Config, database *db.DB) *Handler {
	h := &Handler{service: NewService(cfg, database)}
	router := chi.NewRouter()
	router.NotFound(routeNotFound)
	router.MethodNotAllowed(routeNotFound)
	router.Get("/me", h.getCurrentOrganization)

	router.Route("/invites", func(r chi.Router) {
		r.Post("/", h.createInvite)
		r.Get("/", h.listInvites)
		r.Get("/{invite_id}", h.getInvite)
		r.Delete("/{invite_id}", h.deleteInvite)
	})
	router.Route("/users", func(r chi.Router) {
		r.Get("/", h.listUsers)
		r.Get("/{user_id}", h.getUser)
		r.Post("/{user_id}", h.updateUser)
		r.Delete("/{user_id}", h.deleteUser)
	})
	router.Route("/workspaces", func(r chi.Router) {
		r.Post("/", h.createWorkspace)
		r.Get("/", h.listWorkspaces)
		r.Get("/{workspace_id}", h.getWorkspace)
		r.Post("/{workspace_id}", h.updateWorkspace)
		r.Post("/{workspace_id}/archive", h.archiveWorkspace)
		r.Get("/{workspace_id}/rate_limits", h.listWorkspaceRateLimits)
		r.Route("/{workspace_id}/members", func(r chi.Router) {
			r.Post("/", h.createWorkspaceMember)
			r.Get("/", h.listWorkspaceMembers)
			r.Get("/{user_id}", h.getWorkspaceMember)
			r.Post("/{user_id}", h.updateWorkspaceMember)
			r.Delete("/{user_id}", h.deleteWorkspaceMember)
		})
	})
	router.Get("/rate_limits", h.listOrganizationRateLimits)
	router.Route("/api_keys", func(r chi.Router) {
		r.Get("/", h.listAPIKeys)
		r.Get("/{api_key_id}", h.getAPIKey)
		r.Post("/{api_key_id}", h.updateAPIKey)
	})
	router.Route("/external_keys", func(r chi.Router) {
		r.Post("/", h.createExternalKey)
		r.Get("/", h.listExternalKeys)
		r.Get("/{external_key_id}", h.getExternalKey)
		r.Post("/{external_key_id}", h.updateExternalKey)
		r.Delete("/{external_key_id}", h.deleteExternalKey)
		r.Post("/{external_key_id}/validate", h.validateExternalKey)
	})
	router.Route("/usage_report", func(r chi.Router) {
		r.Get("/messages", h.messagesUsageReport)
		r.Get("/claude_code", h.claudeCodeUsageReport)
	})
	router.Get("/cost_report", h.costReport)
	router.Route("/tunnels", func(r chi.Router) {
		r.Use(h.requireTunnelsBeta)
		r.Get("/", h.listTunnels)
		r.Get("/{tunnel_id}", h.getTunnel)
		r.Post("/{tunnel_id}/reveal_token", h.revealTunnelToken)
		r.Post("/{tunnel_id}/rotate_token", h.rotateTunnelToken)
		r.Post("/{tunnel_id}/archive", h.archiveTunnel)
		r.Route("/{tunnel_id}/certificates", func(r chi.Router) {
			r.Post("/", h.createTunnelCertificate)
			r.Get("/", h.listTunnelCertificates)
			r.Get("/{certificate_id}", h.getTunnelCertificate)
			r.Post("/{certificate_id}/archive", h.archiveTunnelCertificate)
		})
	})
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func routeNotFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (h *Handler) getCurrentOrganization(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.GetCurrentOrganization(r.Context(), principal)
	h.respond(w, r, value, err)
}

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) {
	var req createInviteRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.CreateInvite(r.Context(), principal, req)
	h.respond(w, r, value, err)
}

func (h *Handler) getInvite(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.GetInvite(r.Context(), principal, chi.URLParam(r, "invite_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) listInvites(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.limit(w, r)
	if !ok {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ListInvites(r.Context(), principal, r.URL.Query().Get("after_id"), r.URL.Query().Get("before_id"), limit)
	h.respond(w, r, value, err)
}

func (h *Handler) deleteInvite(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.DeleteInvite(r.Context(), principal, chi.URLParam(r, "invite_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.GetUser(r.Context(), principal, chi.URLParam(r, "user_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.limit(w, r)
	if !ok {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ListUsers(r.Context(), principal, r.URL.Query().Get("email"), r.URL.Query().Get("after_id"), r.URL.Query().Get("before_id"), limit)
	h.respond(w, r, value, err)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	var req updateUserRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.UpdateUser(r.Context(), principal, chi.URLParam(r, "user_id"), req)
	h.respond(w, r, value, err)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.DeleteUser(r.Context(), principal, chi.URLParam(r, "user_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var req createWorkspaceRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.CreateWorkspace(r.Context(), principal, req)
	h.respond(w, r, value, err)
}

func (h *Handler) getWorkspace(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.GetWorkspace(r.Context(), principal, chi.URLParam(r, "workspace_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.limit(w, r)
	if !ok {
		return
	}
	includeArchived, ok := parseBoolQuery(w, r, "include_archived")
	if !ok {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ListWorkspaces(r.Context(), principal, includeArchived, r.URL.Query().Get("after_id"), r.URL.Query().Get("before_id"), limit)
	h.respond(w, r, value, err)
}

func (h *Handler) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	var req updateWorkspaceRequest
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.UpdateWorkspace(r.Context(), principal, chi.URLParam(r, "workspace_id"), req)
	h.respond(w, r, value, err)
}

func (h *Handler) archiveWorkspace(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ArchiveWorkspace(r.Context(), principal, chi.URLParam(r, "workspace_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) createWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	var req createWorkspaceMemberRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.CreateWorkspaceMember(r.Context(), principal, chi.URLParam(r, "workspace_id"), req)
	h.respond(w, r, value, err)
}

func (h *Handler) getWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.GetWorkspaceMember(r.Context(), principal, chi.URLParam(r, "workspace_id"), chi.URLParam(r, "user_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) listWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.limit(w, r)
	if !ok {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ListWorkspaceMembers(r.Context(), principal, chi.URLParam(r, "workspace_id"), r.URL.Query().Get("after_id"), r.URL.Query().Get("before_id"), limit)
	h.respond(w, r, value, err)
}

func (h *Handler) updateWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	var req updateWorkspaceMemberRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.UpdateWorkspaceMember(r.Context(), principal, chi.URLParam(r, "workspace_id"), chi.URLParam(r, "user_id"), req)
	h.respond(w, r, value, err)
}

func (h *Handler) deleteWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.DeleteWorkspaceMember(r.Context(), principal, chi.URLParam(r, "workspace_id"), chi.URLParam(r, "user_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) getAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.GetAPIKey(r.Context(), principal, chi.URLParam(r, "api_key_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.limit(w, r)
	if !ok {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	value, err := h.service.ListAPIKeys(r.Context(), principal, query.Get("workspace_id"), query.Get("created_by_user_id"), query.Get("status"), query.Get("after_id"), query.Get("before_id"), limit)
	h.respond(w, r, value, err)
}

func (h *Handler) updateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req updateAPIKeyRequest
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.UpdateAPIKey(r.Context(), principal, chi.URLParam(r, "api_key_id"), req)
	h.respond(w, r, value, err)
}

func (h *Handler) createExternalKey(w http.ResponseWriter, r *http.Request) {
	var req createExternalKeyRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.CreateExternalKey(r.Context(), principal, req)
	h.respond(w, r, value, err)
}

func (h *Handler) listExternalKeys(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := tokenPageParams(w, r, 20, 1000)
	if !ok {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ListExternalKeys(r.Context(), principal, limit, offset)
	h.respond(w, r, value, err)
}

func (h *Handler) getExternalKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.GetExternalKey(r.Context(), principal, chi.URLParam(r, "external_key_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) updateExternalKey(w http.ResponseWriter, r *http.Request) {
	var req updateExternalKeyRequest
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.UpdateExternalKey(r.Context(), principal, chi.URLParam(r, "external_key_id"), req)
	h.respond(w, r, value, err)
}

func (h *Handler) deleteExternalKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.DeleteExternalKey(r.Context(), principal, chi.URLParam(r, "external_key_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) validateExternalKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ValidateExternalKey(r.Context(), principal, chi.URLParam(r, "external_key_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) listOrganizationRateLimits(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.ListRateLimits(r.URL.Query().Get("model"), r.URL.Query().Get("group_type"), false)
	h.respond(w, r, value, err)
}

func (h *Handler) listWorkspaceRateLimits(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.ListRateLimits("", r.URL.Query().Get("group_type"), true)
	h.respond(w, r, value, err)
}

func (h *Handler) messagesUsageReport(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.MessagesUsageReport(reportQueryFromRequest(r))
	h.respond(w, r, value, err)
}

func (h *Handler) claudeCodeUsageReport(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.ClaudeCodeUsageReport(reportQueryFromRequest(r))
	h.respond(w, r, value, err)
}

func (h *Handler) costReport(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.CostReport(reportQueryFromRequest(r))
	h.respond(w, r, value, err)
}

func (h *Handler) getTunnel(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.GetTunnel(r.Context(), principal, chi.URLParam(r, "tunnel_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) listTunnels(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := tokenPageParams(w, r, 20, 1000)
	if !ok {
		return
	}
	includeArchived, ok := parseBoolQuery(w, r, "include_archived")
	if !ok {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ListTunnels(r.Context(), principal, r.URL.Query().Get("workspace_id"), includeArchived, limit, offset)
	h.respond(w, r, value, err)
}

func (h *Handler) revealTunnelToken(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.RevealTunnelToken(r.Context(), principal, chi.URLParam(r, "tunnel_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) rotateTunnelToken(w http.ResponseWriter, r *http.Request) {
	var req rotateTunnelTokenRequest
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.RotateTunnelToken(r.Context(), principal, chi.URLParam(r, "tunnel_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) archiveTunnel(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ArchiveTunnel(r.Context(), principal, chi.URLParam(r, "tunnel_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) createTunnelCertificate(w http.ResponseWriter, r *http.Request) {
	var req createTunnelCertificateRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.CreateTunnelCertificate(r.Context(), principal, chi.URLParam(r, "tunnel_id"), req)
	h.respond(w, r, value, err)
}

func (h *Handler) getTunnelCertificate(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.GetTunnelCertificate(r.Context(), principal, chi.URLParam(r, "tunnel_id"), chi.URLParam(r, "certificate_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) listTunnelCertificates(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := tokenPageParams(w, r, 20, 1000)
	if !ok {
		return
	}
	includeArchived, ok := parseBoolQuery(w, r, "include_archived")
	if !ok {
		return
	}
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ListTunnelCertificates(r.Context(), principal, chi.URLParam(r, "tunnel_id"), includeArchived, limit, offset)
	h.respond(w, r, value, err)
}

func (h *Handler) archiveTunnelCertificate(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	value, err := h.service.ArchiveTunnelCertificate(r.Context(), principal, chi.URLParam(r, "tunnel_id"), chi.URLParam(r, "certificate_id"))
	h.respond(w, r, value, err)
}

func (h *Handler) requireTunnelsBeta(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hasTunnelsBetaValues(r.Header.Values("anthropic-beta")) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Tunnel endpoints require anthropic-beta: "+tunnelsBeta))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) principal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.CredentialType != "api_key" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return auth.Principal{}, false
	}
	return principal, true
}

func (h *Handler) limit(w http.ResponseWriter, r *http.Request) (int, bool) {
	limit, err := parseCursorLimit(r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return 0, false
	}
	return limit, true
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, value any, err error) {
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, value)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var serviceErr *serviceError
	if errors.As(err, &serviceErr) {
		httpapi.WriteError(w, r, httpapi.NewError(serviceErr.status, serviceErr.typ, serviceErr.message))
		return
	}
	log.Printf("admin api: %v", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Internal server error"))
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any, allowEmpty bool) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return true
		}
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Invalid JSON body"))
		return false
	}
	return true
}

func parseBoolQuery(w http.ResponseWriter, r *http.Request, name string) (bool, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return false, true
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", name+" must be a boolean"))
		return false, false
	}
	return value, true
}

func tokenPageParams(w http.ResponseWriter, r *http.Request, fallback, max int) (int, int, bool) {
	limit, err := parseTokenLimit(r, fallback, max)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return 0, 0, false
	}
	offset, err := decodePageOffset(r.URL.Query().Get("page"))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return 0, 0, false
	}
	return limit, offset, true
}

func reportQueryFromRequest(r *http.Request) reportQuery {
	query := r.URL.Query()
	limit := 0
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil {
			limit = parsed
		}
	}
	return reportQuery{
		StartingAt:  query.Get("starting_at"),
		EndingAt:    query.Get("ending_at"),
		BucketWidth: query.Get("bucket_width"),
		Limit:       limit,
		Page:        query.Get("page"),
	}
}

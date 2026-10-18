package platformadmin

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

type SessionLoader func(*http.Request) (platformsession.Session, error)

type Handler struct {
	service *Service
	load    SessionLoader
	verify  func(context.Context, string) (db.PlatformAdminAccount, error)
	enabled bool
	mu      sync.Mutex
	lookups map[string]time.Time
}

type accountContextKey struct{}
type Account struct {
	UUID  string `json:"uuid"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
type bootstrap struct {
	Account        Account `json:"account"`
	PlatformAccess Access  `json:"platform_access"`
	CSRFToken      string  `json:"csrf_token"`
}

func NewHandler(service *Service, load SessionLoader, enabled bool) *Handler {
	return &Handler{service: service, load: load, verify: service.Authorize, enabled: enabled, lookups: make(map[string]time.Time)}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(h.authorize)
	r.Get("/bootstrap", h.bootstrap)
	r.Get("/accounts", h.accounts)
	r.Get("/organizations", h.organizations)
	r.Post("/organizations", h.createOrganization)
	r.Route("/organizations/{organizationUuid}", func(r chi.Router) {
		r.Use(h.organizationScope)
		r.Get("/", h.organization)
		r.Patch("/", h.updateOrganization)
		r.With(h.requireOrganization).Get("/members", h.members)
		r.With(h.requireOrganization).Get("/workspaces", h.workspaces)
		r.Post("/administrators", h.grantAdministrator)
		r.Patch("/administrators/{memberUuid}", h.demoteAdministrator)
		r.Post("/administrator-transfers", h.transferAdministrator)
		r.With(h.requireOrganization).Get("/invitations", h.invites)
		r.Post("/invitations", h.createInvite)
		r.Post("/invitations/{invitationUuid}/resend", h.changeInvite(true))
		r.Post("/invitations/{invitationUuid}/revoke", h.changeInvite(false))
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { h.fail(w, r, ErrNotFound) })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) { h.fail(w, r, invalid("不支持的请求方法")) })
	return r
}

func (h *Handler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("X-Api-Key") != "" || r.Header.Get("Authorization") != "" {
			h.fail(w, r, ErrDenied)
			return
		}
		session, err := h.load(r)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		if !h.enabled || !session.VerifiedLogin() {
			h.fail(w, r, ErrVerifiedLogin)
			return
		}
		account, err := h.verify(r.Context(), session.UserUUID)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions &&
			!auth.ValidatePlatformCSRFToken(auth.ExtractPlatformSessionKey(r), r.Header.Get("X-CSRF-Token")) {
			h.fail(w, r, ErrCSRF)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accountContextKey{}, account)))
	})
}

func (h *Handler) organizationScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org := chi.URLParam(r, "organizationUuid")
		if _, err := uuid.Parse(org); err != nil {
			h.fail(w, r, invalid("组织 UUID 无效"))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) requireOrganization(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h.service.db.CheckPlatformAdminOrganization(r.Context(), chi.URLParam(r, "organizationUuid")); err != nil {
			h.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func actorFromRequest(r *http.Request) Actor {
	account, _ := r.Context().Value(accountContextKey{}).(db.PlatformAdminAccount)
	return Actor{AccountUUID: account.UUID}
}

func (h *Handler) bootstrap(w http.ResponseWriter, r *http.Request) {
	account, _ := r.Context().Value(accountContextKey{}).(db.PlatformAdminAccount)
	respond(w, http.StatusOK, bootstrap{Account: Account{account.UUID, account.Name, account.Email},
		PlatformAccess: Permissions(true), CSRFToken: auth.PlatformCSRFToken(auth.ExtractPlatformSessionKey(r))})
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	e := publicError(err)
	if e.Status >= 500 {
		h.service.logger.ErrorContext(r.Context(), "platform administration failed", "request_id", httpapi.RequestID(r.Context()), "error", err)
	}
	if e.Status == 403 {
		h.service.logger.WarnContext(r.Context(), "platform administration denied", "request_id", httpapi.RequestID(r.Context()), "code", e.Code)
	}
	respond(w, e.Status, struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}{
		Error: struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{e.Code, e.Message}, RequestID: httpapi.RequestID(r.Context()),
	})
}

func respond(w http.ResponseWriter, status int, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		http.Error(w, "response encoding failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func decodeRequest[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var input T
	err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 64*1024), &input, json.RejectUnknownMembers(true))
	if err != nil {
		return input, invalid("请求字段或 JSON 格式无效")
	}
	return input, nil
}

func (h *Handler) writeChange(w http.ResponseWriter, r *http.Request, result Organization, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond(w, http.StatusOK, result)
}

func (h *Handler) createOrganization(w http.ResponseWriter, r *http.Request) {
	input, err := decodeRequest[CreateOrganization](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	input.Name, input.Reason = strings.TrimSpace(input.Name), strings.TrimSpace(input.Reason)
	result, replayed, err := h.service.CreateOrganization(r.Context(), actorFromRequest(r), input, strings.TrimSpace(r.Header.Get("Idempotency-Key")))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	respond(w, status, result)
}

func (h *Handler) updateOrganization(w http.ResponseWriter, r *http.Request) {
	input, err := decodeRequest[OrganizationPatch](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	for _, field := range []*string{input.Name, input.Website, input.Industry} {
		if field != nil {
			*field = strings.TrimSpace(*field)
		}
	}
	result, err := h.service.UpdateOrganization(r.Context(), actorFromRequest(r), chi.URLParam(r, "organizationUuid"), input)
	h.writeChange(w, r, result, err)
}

func (h *Handler) grantAdministrator(w http.ResponseWriter, r *http.Request) {
	input, err := decodeRequest[AdministratorChange](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	result, err := h.service.GrantAdministrator(r.Context(), actorFromRequest(r), chi.URLParam(r, "organizationUuid"), input)
	h.writeChange(w, r, result, err)
}

func (h *Handler) demoteAdministrator(w http.ResponseWriter, r *http.Request) {
	input, err := decodeRequest[AdministratorChange](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if _, err := uuid.Parse(chi.URLParam(r, "memberUuid")); err != nil {
		h.fail(w, r, invalid("成员 UUID 无效"))
		return
	}
	result, err := h.service.DemoteAdministrator(r.Context(), actorFromRequest(r), chi.URLParam(r, "organizationUuid"), chi.URLParam(r, "memberUuid"), input)
	h.writeChange(w, r, result, err)
}

func (h *Handler) transferAdministrator(w http.ResponseWriter, r *http.Request) {
	input, err := decodeRequest[AdministratorChange](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	result, err := h.service.TransferAdministrator(r.Context(), actorFromRequest(r), chi.URLParam(r, "organizationUuid"), input)
	h.writeChange(w, r, result, err)
}

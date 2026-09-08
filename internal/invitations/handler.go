// Package invitations 提供以服务端已验证邮箱为身份边界的邀请资源。
package invitations

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type Store interface {
	ListInvitations(context.Context, string) ([]db.Invitation, error)
	RespondToInvitation(context.Context, string, string, bool) (db.Invitation, error)
}

type Handler struct {
	store         Store
	verifiedEmail func(*http.Request) (string, bool)
	errors        *httpapi.ErrorAdapter
}

func NewHandler(store Store, verifiedEmail func(*http.Request) (string, bool), logger *slog.Logger) *Handler {
	return &Handler{store: store, verifiedEmail: verifiedEmail, errors: httpapi.NewErrorAdapter(logger)}
}

// RegisterRoutes 注册相对资源路由；调用方负责登录身份与写请求 CSRF 校验。
func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Get("/", h.errors.Wrap(h.list))
	router.Post("/{id}/accept", h.errors.Wrap(func(w http.ResponseWriter, r *http.Request) error { return h.respond(w, r, true) }))
	router.Post("/{id}/decline", h.errors.Wrap(func(w http.ResponseWriter, r *http.Request) error { return h.respond(w, r, false) }))
}

type invitationResponse struct {
	ID               string    `json:"id"`
	OrganizationUUID string    `json:"organization_uuid"`
	OrganizationName string    `json:"organization_name"`
	Role             string    `json:"role"`
	InvitedAt        time.Time `json:"invited_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type responseResult struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	OrganizationUUID string `json:"organization_uuid"`
	UserID           string `json:"user_id,omitempty"`
	Role             string `json:"role,omitempty"`
}

func (h *Handler) email(r *http.Request) (string, bool) {
	if h.verifiedEmail == nil {
		return "", false
	}
	email, ok := h.verifiedEmail(r)
	return email, ok && email != ""
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	email, ok := h.email(r)
	if !ok {
		return verifiedEmailRequired()
	}
	rows, err := h.store.ListInvitations(r.Context(), email)
	if err != nil {
		return mapDBError(err)
	}
	data := make([]invitationResponse, 0, len(rows))
	for _, row := range rows {
		data = append(data, invitationResponse{ID: row.ID, OrganizationUUID: row.OrganizationUUID, OrganizationName: row.OrganizationName, Role: row.Role, InvitedAt: row.InvitedAt, ExpiresAt: row.ExpiresAt})
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Data []invitationResponse `json:"data"`
	}{Data: data})
	return nil
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, accept bool) error {
	email, ok := h.email(r)
	if !ok {
		return verifiedEmailRequired()
	}
	row, err := h.store.RespondToInvitation(r.Context(), chi.URLParam(r, "id"), email, accept)
	if err != nil {
		return mapDBError(err)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseResult{ID: row.ID, Status: row.Status, OrganizationUUID: row.OrganizationUUID, UserID: row.UserID, Role: row.Role})
	return nil
}

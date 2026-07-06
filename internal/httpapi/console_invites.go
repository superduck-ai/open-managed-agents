package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type consoleInviteStore interface {
	ListConsoleInvites(ctx context.Context, orgUUID string, status string, limit int) ([]ConsoleInvite, error)
	CreateConsoleInvite(ctx context.Context, input CreateConsoleInviteInput) (ConsoleInvite, error)
	ResendConsoleInvite(ctx context.Context, orgUUID string, inviteID string) (ConsoleInvite, error)
	DeleteConsoleInvite(ctx context.Context, orgUUID string, inviteID string) (ConsoleInvite, error)
}

type createConsoleInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func RegisterConsoleOrganizationInviteRoutes(r chi.Router, store OrganizationStore) {
	registerConsoleOrganizationInviteRoutes(r, store)
}

func registerConsoleOrganizationInviteRoutes(r chi.Router, store OrganizationStore) {
	r.Get("/invites", handleListConsoleInvites(store))
	r.Post("/invites", handleCreateConsoleInvite(store))
	r.Put("/invites/{inviteId}", handleResendConsoleInvite(store))
	r.Delete("/invites/{inviteId}", handleDeleteConsoleInvite(store))
}

func handleListConsoleInvites(store OrganizationStore) http.HandlerFunc {
	inviteStore, _ := store.(consoleInviteStore)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if inviteStore == nil {
			internalError(w, "failed to list invites")
			return
		}
		status, ok := normalizeConsoleInviteStatusFilter(r.URL.Query().Get("status"))
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid status"})
			return
		}
		invites, err := inviteStore.ListConsoleInvites(r.Context(), orgUUID, status, 1000)
		if err != nil {
			internalError(w, "failed to list invites")
			return
		}
		out := make([]map[string]any, 0, len(invites))
		for _, invite := range invites {
			out = append(out, formatConsoleInvite(invite))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func handleCreateConsoleInvite(store OrganizationStore) http.HandlerFunc {
	inviteStore, _ := store.(consoleInviteStore)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if inviteStore == nil {
			internalError(w, "failed to create invite")
			return
		}
		body, err := readRequiredJSON[createConsoleInviteRequest](r, true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_request",
				"message": "request body must match CreateConsoleInviteRequest",
			})
			return
		}
		email, ok := normalizeConsoleInviteEmail(body.Email)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid email"})
			return
		}
		role := normalizeConsoleMemberRole(body.Role)
		if role == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid role"})
			return
		}
		invite, err := inviteStore.CreateConsoleInvite(r.Context(), CreateConsoleInviteInput{
			OrgUUID: orgUUID,
			Email:   email,
			Role:    role,
		})
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				organizationNotFound(w)
				return
			}
			internalError(w, "failed to create invite")
			return
		}
		writeJSON(w, http.StatusOK, formatConsoleInvite(invite))
	}
}

func handleResendConsoleInvite(store OrganizationStore) http.HandlerFunc {
	inviteStore, _ := store.(consoleInviteStore)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if inviteStore == nil {
			internalError(w, "failed to resend invite")
			return
		}
		inviteID := strings.TrimSpace(chi.URLParam(r, "inviteId"))
		if inviteID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invite_id_required"})
			return
		}
		invite, err := inviteStore.ResendConsoleInvite(r.Context(), orgUUID, inviteID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "invite not found"})
				return
			}
			internalError(w, "failed to resend invite")
			return
		}
		writeJSON(w, http.StatusOK, formatConsoleInvite(invite))
	}
}

func handleDeleteConsoleInvite(store OrganizationStore) http.HandlerFunc {
	inviteStore, _ := store.(consoleInviteStore)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if inviteStore == nil {
			internalError(w, "failed to delete invite")
			return
		}
		inviteID := strings.TrimSpace(chi.URLParam(r, "inviteId"))
		if inviteID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invite_id_required"})
			return
		}
		invite, err := inviteStore.DeleteConsoleInvite(r.Context(), orgUUID, inviteID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "invite not found"})
				return
			}
			internalError(w, "failed to delete invite")
			return
		}
		writeJSON(w, http.StatusOK, formatDeletedConsoleInvite(invite.ID))
	}
}

func formatConsoleInvite(invite ConsoleInvite) map[string]any {
	return map[string]any{
		"id":         invite.ID,
		"type":       "invite",
		"email":      invite.Email,
		"role":       invite.Role,
		"invited_at": isoTime(invite.InvitedAt),
		"expires_at": isoTime(invite.ExpiresAt),
		"status":     effectiveConsoleInviteStatus(invite),
	}
}

func formatDeletedConsoleInvite(inviteID string) map[string]any {
	return map[string]any{
		"id":   inviteID,
		"type": "invite_deleted",
	}
}

func effectiveConsoleInviteStatus(invite ConsoleInvite) string {
	status := strings.TrimSpace(strings.ToLower(invite.Status))
	if status == "" {
		status = "pending"
	}
	if status == "pending" && !invite.ExpiresAt.IsZero() && time.Now().UTC().After(invite.ExpiresAt) {
		return "expired"
	}
	return status
}

func normalizeConsoleInviteStatusFilter(status string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "":
		return "", true
	case "pending", "accepted", "expired", "deleted":
		return strings.TrimSpace(strings.ToLower(status)), true
	default:
		return "", false
	}
}

func normalizeConsoleInviteEmail(email string) (string, bool) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "", false
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Address == "" {
		return "", false
	}
	if parsed.Name != "" && parsed.String() != parsed.Address {
		return "", false
	}
	return strings.ToLower(parsed.Address), true
}

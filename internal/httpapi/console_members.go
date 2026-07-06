package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type consoleMemberStore interface {
	ListOrgUsers(ctx context.Context, orgUUID string, limit int) ([]OrgUser, error)
	UpdateOrgUserRole(ctx context.Context, orgUUID string, userID string, role string) (*OrgUser, error)
	RemoveOrgUser(ctx context.Context, orgUUID string, userID string) (bool, error)
}

type updateConsoleMemberRequest struct {
	Role string `json:"role"`
}

func RegisterConsoleOrganizationMemberRoutes(r chi.Router, store OrganizationStore) {
	registerConsoleOrganizationMemberRoutes(r, store)
}

func registerConsoleOrganizationMemberRoutes(r chi.Router, store OrganizationStore) {
	r.Get("/members", handleListConsoleMembers(store))
	r.Post("/members/{userId}", handleUpdateConsoleMember(store))
	r.Delete("/members/{userId}", handleDeleteConsoleMember(store))
}

func handleListConsoleMembers(store OrganizationStore) http.HandlerFunc {
	memberStore, _ := store.(consoleMemberStore)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if memberStore == nil {
			internalError(w, "failed to list members")
			return
		}
		users, err := memberStore.ListOrgUsers(r.Context(), orgUUID, 1000)
		if err != nil {
			internalError(w, "failed to list members")
			return
		}
		members := make([]map[string]any, 0, len(users))
		for _, user := range users {
			members = append(members, formatConsoleMember(user))
		}
		writeJSON(w, http.StatusOK, members)
	}
}

func formatConsoleMember(user OrgUser) map[string]any {
	return map[string]any{
		"id":       taggedUserID(user.UserUUID),
		"type":     "user",
		"email":    user.Email,
		"name":     consoleMemberName(user),
		"role":     consoleMemberRole(user.Role),
		"added_at": isoTime(user.AddedAt),
	}
}

func consoleMemberName(user OrgUser) string {
	if user.FullName != nil && strings.TrimSpace(*user.FullName) != "" {
		return *user.FullName
	}
	return user.Email
}

func consoleMemberRole(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "admin", "owner", "primary_owner", "membership_admin":
		return "admin"
	case "developer", "billing", "claude_code_user":
		return strings.TrimSpace(strings.ToLower(role))
	default:
		return "user"
	}
}

func handleUpdateConsoleMember(store OrganizationStore) http.HandlerFunc {
	memberStore, _ := store.(consoleMemberStore)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if memberStore == nil {
			internalError(w, "failed to update member")
			return
		}
		body, err := readRequiredJSON[updateConsoleMemberRequest](r, true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_request",
				"message": "request body must match UpdateConsoleMemberRequest",
			})
			return
		}
		role := normalizeConsoleMemberRole(body.Role)
		if role == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid role"})
			return
		}
		userID := strings.TrimSpace(chi.URLParam(r, "userId"))
		if userID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "user_id_required"})
			return
		}
		user, err := memberStore.UpdateOrgUserRole(r.Context(), orgUUID, userID, role)
		if err != nil {
			internalError(w, "failed to update member")
			return
		}
		if user == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
			return
		}
		writeJSON(w, http.StatusOK, formatConsoleMember(*user))
	}
}

func handleDeleteConsoleMember(store OrganizationStore) http.HandlerFunc {
	memberStore, _ := store.(consoleMemberStore)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if memberStore == nil {
			internalError(w, "failed to remove member")
			return
		}
		userID := strings.TrimSpace(chi.URLParam(r, "userId"))
		if userID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "user_id_required"})
			return
		}
		if _, err := memberStore.RemoveOrgUser(r.Context(), orgUUID, userID); err != nil {
			internalError(w, "failed to remove member")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": userID, "type": "user_deleted"})
	}
}

func normalizeConsoleMemberRole(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "user", "developer", "billing", "admin", "claude_code_user":
		return strings.TrimSpace(strings.ToLower(role))
	case "member":
		return "user"
	default:
		return ""
	}
}

func taggedUserID(userUUID string) string {
	compact := strings.ReplaceAll(userUUID, "-", "")
	if len(compact) > 24 {
		compact = compact[:24]
	}
	return "user_" + compact
}

package platformapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

type consoleWorkspaceMemberStore interface {
	workspaceaccess.Store
	workspaceaccess.MemberStore
	ListOrgUsers(ctx context.Context, orgUUID string, limit int) ([]platform.OrgUser, error)
	FindOrgMemberByReference(ctx context.Context, orgUUID, userReference string) (db.AdminUser, error)
	ListWorkspaceMemberFacts(ctx context.Context, orgUUID, workspaceUUID string) ([]db.WorkspaceMemberFact, error)
}

type createConsoleWorkspaceMemberRequest struct {
	UserID        string `json:"user_id"`
	WorkspaceRole string `json:"workspace_role"`
}

type updateConsoleWorkspaceMemberRequest struct {
	WorkspaceRole string `json:"workspace_role"`
}

func RegisterConsoleWorkspaceMemberRoutes(r chi.Router, store OrganizationStore) {
	r.Get("/workspaces/{workspaceId}/members", handleListConsoleWorkspaceMembers(store))
	r.Get("/workspaces/{workspaceId}/member-candidates", handleListConsoleWorkspaceMemberCandidates(store))
	r.Post("/workspaces/{workspaceId}/members", handleCreateConsoleWorkspaceMember(store))
	r.Post("/workspaces/{workspaceId}/members/{userId}", handleUpdateConsoleWorkspaceMember(store))
	r.Delete("/workspaces/{workspaceId}/members/{userId}", handleDeleteConsoleWorkspaceMember(store))
}

func handleListConsoleWorkspaceMembers(store OrganizationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		memberStore, ok := consoleMemberChangeStore(w, store)
		if !ok {
			return
		}
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		principal, _ := auth.PrincipalFromContext(r.Context())
		workspace, access, ok := resolveConsoleWorkspace(w, r, memberStore, orgUUID, principal.UserExternalID, chi.URLParam(r, "workspaceId"))
		if !ok {
			return
		}
		facts, err := memberStore.ListWorkspaceMemberFacts(r.Context(), orgUUID, workspace.UUID)
		if err != nil {
			internalError(w, "failed to list workspace members")
			return
		}
		users, err := memberStore.ListOrgUsers(r.Context(), orgUUID, 1000)
		if err != nil {
			internalError(w, "failed to list workspace members")
			return
		}
		profiles := make(map[string]platform.OrgUser, len(users))
		for _, user := range users {
			profiles[user.UserUUID] = user
		}
		members := make([]map[string]any, 0, len(facts))
		for _, fact := range facts {
			memberAccess, accessErr := workspaceaccess.Effective(fact.OrganizationRole, workspace.IsDefault, fact.ExplicitRole)
			if accessErr != nil {
				continue
			}
			profile := profiles[fact.UserUUID]
			members = append(members, formatConsoleWorkspaceMember(fact, memberAccess, profile, access.ManageMembers()))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"workspace_id":       workspace.ExternalID,
			"is_default":         workspace.IsDefault,
			"can_manage_members": access.ManageMembers(),
			"members":            members,
		})
	}
}

func handleListConsoleWorkspaceMemberCandidates(store OrganizationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		memberStore, ok := consoleMemberChangeStore(w, store)
		if !ok {
			return
		}
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		principal, _ := auth.PrincipalFromContext(r.Context())
		workspace, access, ok := resolveConsoleWorkspace(w, r, memberStore, orgUUID, principal.UserExternalID, chi.URLParam(r, "workspaceId"))
		if !ok {
			return
		}
		if !access.ManageMembers() {
			writeConsoleWorkspaceMemberError(w, workspaceaccess.ErrDenied)
			return
		}
		users, err := memberStore.ListOrgUsers(r.Context(), orgUUID, 1000)
		if err != nil {
			internalError(w, "failed to list member candidates")
			return
		}
		facts, err := memberStore.ListWorkspaceMemberFacts(r.Context(), orgUUID, workspace.UUID)
		if err != nil {
			internalError(w, "failed to list member candidates")
			return
		}
		explicit := make(map[string]bool, len(facts))
		for _, fact := range facts {
			if fact.ExplicitRole != "" {
				explicit[fact.UserUUID] = true
			}
		}
		candidates := make([]map[string]any, 0, len(users))
		for _, user := range users {
			role := strings.ToLower(strings.TrimSpace(user.Role))
			if role == "admin" || role == "billing" || explicit[user.UserUUID] {
				continue
			}
			candidates = append(candidates, map[string]any{
				"user_id": taggedUserID(user.UserUUID),
				"name":    consoleMemberName(user),
				"email":   user.Email,
			})
		}
		writeJSON(w, http.StatusOK, candidates)
	}
}

func handleCreateConsoleWorkspaceMember(store OrganizationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readRequiredJSON[createConsoleWorkspaceMemberRequest](r, true)
		if err != nil || body.UserID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "user_id and workspace_role are required"})
			return
		}
		applyConsoleWorkspaceMemberChange(w, r, store, body.UserID, body.WorkspaceRole, "create")
	}
}

func handleUpdateConsoleWorkspaceMember(store OrganizationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readRequiredJSON[updateConsoleWorkspaceMemberRequest](r, true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "workspace_role is required"})
			return
		}
		applyConsoleWorkspaceMemberChange(w, r, store, chi.URLParam(r, "userId"), body.WorkspaceRole, "update")
	}
}

func handleDeleteConsoleWorkspaceMember(store OrganizationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		applyConsoleWorkspaceMemberChange(w, r, store, chi.URLParam(r, "userId"), "", "delete")
	}
}

func applyConsoleWorkspaceMemberChange(w http.ResponseWriter, r *http.Request, store OrganizationStore, userID, role, operation string) {
	memberStore, ok := consoleMemberChangeStore(w, store)
	if !ok {
		return
	}
	orgUUID, ok := visibleOrgUUID(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	workspace, _, ok := resolveConsoleWorkspace(w, r, memberStore, orgUUID, principal.UserExternalID, chi.URLParam(r, "workspaceId"))
	if !ok {
		return
	}
	target, err := memberStore.FindOrgMemberByReference(r.Context(), orgUUID, userID)
	if err != nil {
		writeConsoleWorkspaceMemberError(w, err)
		return
	}
	if _, err := workspaceaccess.ChangeMember(r.Context(), memberStore, principal, workspace.ExternalID, target.ExternalID, role, operation); err != nil {
		writeConsoleWorkspaceMemberError(w, err)
		return
	}
	if operation == "delete" {
		writeJSON(w, http.StatusOK, map[string]any{"id": taggedUserID(target.UUID), "type": "workspace_member_deleted"})
		return
	}
	member, err := memberStore.GetAdminWorkspaceMember(r.Context(), orgUUID, workspace.ExternalID, target.ExternalID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		writeConsoleWorkspaceMemberError(w, err)
		return
	}
	explicitRole := member.WorkspaceRole
	if errors.Is(err, db.ErrNotFound) {
		explicitRole = ""
	}
	memberAccess, accessErr := workspaceaccess.Effective(target.Role, workspace.IsDefault, explicitRole)
	if accessErr != nil {
		writeConsoleWorkspaceMemberError(w, accessErr)
		return
	}
	fact := db.WorkspaceMemberFact{UserUUID: target.UUID, UserExternalID: target.ExternalID, OrganizationRole: target.Role, ExplicitRole: explicitRole}
	writeJSON(w, http.StatusOK, formatConsoleWorkspaceMember(fact, memberAccess, platform.OrgUser{UserUUID: target.UUID, Email: target.Email, FullName: nullableName(target.Name), Role: target.Role}, true))
}

func consoleMemberChangeStore(w http.ResponseWriter, store OrganizationStore) (consoleWorkspaceMemberStore, bool) {
	memberStore, ok := store.(consoleWorkspaceMemberStore)
	if !ok {
		internalError(w, "failed to manage workspace members")
		return nil, false
	}
	return memberStore, true
}

func resolveConsoleWorkspace(w http.ResponseWriter, r *http.Request, store workspaceaccess.Store, orgUUID, userID, workspaceID string) (db.AdminWorkspace, auth.WorkspaceAccess, bool) {
	workspace, access, err := workspaceaccess.New(store).Resolve(r.Context(), orgUUID, userID, workspaceID)
	if errors.Is(err, workspaceaccess.ErrDenied) {
		writeConsoleWorkspaceMemberError(w, err)
		return db.AdminWorkspace{}, auth.WorkspaceAccess{}, false
	}
	if err != nil {
		internalError(w, "failed to resolve workspace")
		return db.AdminWorkspace{}, auth.WorkspaceAccess{}, false
	}
	return workspace, access, true
}

func formatConsoleWorkspaceMember(fact db.WorkspaceMemberFact, access auth.WorkspaceAccess, profile platform.OrgUser, canManage bool) map[string]any {
	canChange := canManage && fact.OrganizationRole != "admin"
	if fact.OrganizationRole == "billing" && access.Source != "billing_override" {
		canChange = false
	}
	name := profile.FullName
	if name == nil || strings.TrimSpace(*name) == "" {
		name = &profile.Email
	}
	return map[string]any{
		"user_id":           taggedUserID(fact.UserUUID),
		"name":              *name,
		"email":             profile.Email,
		"organization_role": consoleMemberRole(fact.OrganizationRole),
		"workspace_role":    access.Role,
		"role_source":       access.Source,
		"can_edit":          canChange,
		"can_remove":        canChange,
	}
}

func nullableName(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func writeConsoleWorkspaceMemberError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "workspace_member_operation_failed", "Could not update workspace membership."
	switch {
	case errors.Is(err, workspaceaccess.ErrDenied):
		status, code, message = http.StatusForbidden, "workspace_access_denied", "You do not have permission to manage this workspace."
	case errors.Is(err, workspaceaccess.ErrDefaultProtected):
		status, code, message = http.StatusConflict, "default_workspace_protected", "Default Workspace membership is managed at the organization level."
	case errors.Is(err, workspaceaccess.ErrInheritedRole):
		status, code, message = http.StatusConflict, "inherited_workspace_role", "Inherited workspace membership cannot be changed or removed."
	case errors.Is(err, workspaceaccess.ErrInvalidRole):
		status, code, message = http.StatusBadRequest, "invalid_workspace_role", "Workspace role is not assignable."
	case errors.Is(err, db.ErrDuplicate):
		status, code, message = http.StatusConflict, "member_exists", "The member already belongs to this workspace."
	case errors.Is(err, db.ErrNotFound):
		status, code, message = http.StatusNotFound, "member_not_found", "Workspace member not found."
	}
	writeJSON(w, status, struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}{Error: code, Message: message})
}

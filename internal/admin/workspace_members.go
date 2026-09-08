package admin

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func (s *Service) CreateWorkspaceMember(ctx context.Context, principal auth.Principal, workspaceID string, req createWorkspaceMemberRequest) (workspaceMemberResponse, error) {
	member, err := workspaceaccess.ChangeMember(ctx, s.db, principal, workspaceID, req.UserID, req.WorkspaceRole, "create")
	return workspaceMemberFromRecord(member), mapAdminDBError(err, "Workspace member not found")
}
func (s *Service) UpdateWorkspaceMember(ctx context.Context, principal auth.Principal, workspaceID, userID string, req updateWorkspaceMemberRequest) (workspaceMemberResponse, error) {
	member, err := workspaceaccess.ChangeMember(ctx, s.db, principal, workspaceID, userID, req.WorkspaceRole, "update")
	return workspaceMemberFromRecord(member), mapAdminDBError(err, "Workspace member not found")
}
func (s *Service) DeleteWorkspaceMember(ctx context.Context, principal auth.Principal, workspaceID, userID string) (map[string]string, error) {
	member, err := workspaceaccess.ChangeMember(ctx, s.db, principal, workspaceID, userID, "", "delete")
	if err != nil {
		return nil, mapAdminDBError(err, "Workspace member not found")
	}
	return map[string]string{"type": "workspace_member_deleted", "user_id": member.UserExternalID, "workspace_id": member.WorkspaceExternalID}, nil
}

func (s *Service) workspaceMembers(ctx context.Context, principal auth.Principal, workspaceID string) ([]workspaceMemberResponse, error) {
	if !principal.WorkspaceAccess.ManageOrganization() {
		_, access, err := workspaceaccess.New(s.db).Resolve(ctx, principal.OrganizationUUID, principal.UserExternalID, workspaceID)
		if err != nil {
			return nil, mapAdminDBError(err, "Workspace not found")
		}
		if !access.ManageMembers() {
			return nil, mapAdminDBError(workspaceaccess.ErrDenied, "Workspace not found")
		}
	}
	workspace, err := s.db.GetAdminWorkspace(ctx, principal.OrganizationUUID, workspaceID)
	if err != nil {
		return nil, mapAdminDBError(err, "Workspace not found")
	}
	facts, err := s.db.ListWorkspaceMemberFacts(ctx, principal.OrganizationUUID, workspace.UUID)
	if err != nil {
		return nil, err
	}
	members := make([]workspaceMemberResponse, 0, len(facts))
	for _, fact := range facts {
		access, accessErr := workspaceaccess.Effective(fact.OrganizationRole, workspace.IsDefault, fact.ExplicitRole)
		if accessErr != nil {
			continue
		}
		members = append(members, workspaceMemberFromRecord(db.AdminWorkspaceMember{
			WorkspaceExternalID: workspace.ExternalID, UserExternalID: fact.UserExternalID, WorkspaceRole: access.Role,
		}))
	}
	return members, nil
}

func (s *Service) GetWorkspaceMember(ctx context.Context, principal auth.Principal, workspaceID, userID string) (workspaceMemberResponse, error) {
	members, err := s.workspaceMembers(ctx, principal, workspaceID)
	if err != nil {
		return workspaceMemberResponse{}, err
	}
	for _, member := range members {
		if member.UserID == userID {
			return member, nil
		}
	}
	return workspaceMemberResponse{}, notFound("Workspace member not found")
}

func (s *Service) ListWorkspaceMembers(ctx context.Context, principal auth.Principal, workspaceID, afterID, beforeID string, limit int) (cursorPageResponse[workspaceMemberResponse], error) {
	members, err := s.workspaceMembers(ctx, principal, workspaceID)
	if err != nil {
		return cursorPageResponse[workspaceMemberResponse]{}, err
	}
	if afterID != "" && beforeID != "" {
		return cursorPageResponse[workspaceMemberResponse]{}, invalidRequest("after_id and before_id are mutually exclusive")
	}
	start, end := 0, len(members)
	if afterID != "" || beforeID != "" {
		found := false
		for index, member := range members {
			if member.UserID == afterID {
				start = index + 1
				found = true
				break
			}
			if member.UserID == beforeID {
				end = index
				found = true
				break
			}
		}
		if !found {
			start, end = 0, 0
		}
	}
	members = members[start:end]
	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}
	return cursorPage(members, hasMore, func(value workspaceMemberResponse) string { return value.UserID }), nil
}

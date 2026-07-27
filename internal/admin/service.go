package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/google/uuid"
)

type Service struct {
	cfg config.Config
	db  *db.DB
}

type serviceError struct {
	status  int
	typ     string
	message string
}

func (e *serviceError) Error() string {
	return e.message
}

func NewService(cfg config.Config, database *db.DB) *Service {
	return &Service{cfg: cfg, db: database}
}

func (s *Service) GetCurrentOrganization(ctx context.Context, principal auth.Principal) (organizationResponse, error) {
	org, err := s.db.GetAdminOrganization(ctx, principal.OrganizationID)
	if err != nil {
		return organizationResponse{}, mapAdminDBError(err, "Organization not found")
	}
	return organizationResponse{ID: org.ExternalID, Name: org.Name, Type: "organization"}, nil
}

func (s *Service) CreateInvite(ctx context.Context, principal auth.Principal, req createInviteRequest) (inviteResponse, error) {
	email, err := normalizeEmail(req.Email)
	if err != nil {
		return inviteResponse{}, invalidRequest(err.Error())
	}
	if err := validateOrganizationRole(req.Role, false); err != nil {
		return inviteResponse{}, invalidRequest(err.Error())
	}
	externalID, err := ids.New("invite_")
	if err != nil {
		return inviteResponse{}, err
	}
	now := time.Now().UTC()
	invite, err := s.db.CreateAdminInvite(ctx, db.AdminInvite{
		ExternalID:     externalID,
		OrganizationID: principal.OrganizationID,
		Email:          email,
		Role:           req.Role,
		Status:         "pending",
		InvitedAt:      now,
		ExpiresAt:      now.Add(21 * 24 * time.Hour),
	})
	if err != nil {
		return inviteResponse{}, mapAdminDBError(err, "Could not create invite")
	}
	return inviteFromRecord(invite), nil
}

func (s *Service) GetInvite(ctx context.Context, principal auth.Principal, inviteID string) (inviteResponse, error) {
	invite, err := s.db.GetAdminInvite(ctx, principal.OrganizationID, inviteID)
	if err != nil {
		return inviteResponse{}, mapAdminDBError(err, "Invite not found")
	}
	return inviteFromRecord(invite), nil
}

func (s *Service) ListInvites(ctx context.Context, principal auth.Principal, afterID, beforeID string, limit int) (cursorPageResponse[inviteResponse], error) {
	records, hasMore, err := s.db.ListAdminInvitesPage(ctx, db.ListAdminInvitesParams{
		OrganizationID: principal.OrganizationID,
		AfterID:        afterID,
		BeforeID:       beforeID,
		Limit:          limit,
	})
	if err != nil {
		return cursorPageResponse[inviteResponse]{}, err
	}
	data := make([]inviteResponse, 0, len(records))
	for _, record := range records {
		data = append(data, inviteFromRecord(record))
	}
	return cursorPage(data, hasMore, func(value inviteResponse) string { return value.ID }), nil
}

func (s *Service) DeleteInvite(ctx context.Context, principal auth.Principal, inviteID string) (map[string]string, error) {
	if _, err := s.db.DeleteAdminInvite(ctx, principal.OrganizationID, inviteID); err != nil {
		return nil, mapAdminDBError(err, "Invite not found")
	}
	return map[string]string{"id": inviteID, "type": "invite_deleted"}, nil
}

func (s *Service) GetUser(ctx context.Context, principal auth.Principal, userID string) (userResponse, error) {
	user, err := s.db.GetAdminUser(ctx, principal.OrganizationID, userID)
	if err != nil {
		return userResponse{}, mapAdminDBError(err, "User not found")
	}
	return userFromRecord(user), nil
}

func (s *Service) ListUsers(ctx context.Context, principal auth.Principal, email, afterID, beforeID string, limit int) (cursorPageResponse[userResponse], error) {
	var err error
	if strings.TrimSpace(email) != "" {
		email, err = normalizeEmail(email)
		if err != nil {
			return cursorPageResponse[userResponse]{}, invalidRequest(err.Error())
		}
	}
	records, hasMore, err := s.db.ListAdminUsersPage(ctx, db.ListAdminUsersParams{
		OrganizationID: principal.OrganizationID,
		Email:          email,
		AfterID:        afterID,
		BeforeID:       beforeID,
		Limit:          limit,
	})
	if err != nil {
		return cursorPageResponse[userResponse]{}, err
	}
	data := make([]userResponse, 0, len(records))
	for _, record := range records {
		data = append(data, userFromRecord(record))
	}
	return cursorPage(data, hasMore, func(value userResponse) string { return value.ID }), nil
}

func (s *Service) UpdateUser(ctx context.Context, principal auth.Principal, userID string, req updateUserRequest) (userResponse, error) {
	if err := validateOrganizationRole(req.Role, false); err != nil {
		return userResponse{}, invalidRequest(err.Error())
	}
	user, err := s.db.UpdateAdminUserRole(ctx, principal.OrganizationID, userID, req.Role)
	if err != nil {
		return userResponse{}, mapAdminDBError(err, "User not found")
	}
	return userFromRecord(user), nil
}

func (s *Service) DeleteUser(ctx context.Context, principal auth.Principal, userID string) (map[string]string, error) {
	if _, err := s.db.DeleteAdminUser(ctx, principal.OrganizationID, userID); err != nil {
		return nil, mapAdminDBError(err, "User not found")
	}
	return map[string]string{"id": userID, "type": "user_deleted"}, nil
}

func (s *Service) CreateWorkspace(ctx context.Context, principal auth.Principal, req createWorkspaceRequest) (workspaceResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return workspaceResponse{}, invalidRequest("name is required")
	}
	if err := validateTags(req.Tags); err != nil {
		return workspaceResponse{}, invalidRequest(err.Error())
	}
	residency, err := dataResidencyFromRequest(req.DataResidency, nil)
	if err != nil {
		return workspaceResponse{}, invalidRequest(err.Error())
	}
	residencyJSON, err := encodeDataResidency(residency)
	if err != nil {
		return workspaceResponse{}, err
	}
	tagsJSON, err := json.Marshal(defaultTags(req.Tags))
	if err != nil {
		return workspaceResponse{}, err
	}
	if req.ExternalKeyID != nil && strings.TrimSpace(*req.ExternalKeyID) != "" {
		if _, err := s.db.GetAdminExternalKey(ctx, principal.OrganizationID, strings.TrimSpace(*req.ExternalKeyID)); err != nil {
			return workspaceResponse{}, mapAdminDBError(err, "External key not found")
		}
	}
	workspaceID, err := ids.New("wrkspc_")
	if err != nil {
		return workspaceResponse{}, err
	}
	now := time.Now().UTC()
	record, err := s.db.CreateAdminWorkspace(ctx, db.AdminWorkspace{
		UUID:           uuid.NewString(),
		ExternalID:     workspaceID,
		OrganizationID: principal.OrganizationID,
		Name:           name,
		CreatedAt:      now,
		CompartmentID:  uuid.NewString(),
		DisplayColor:   "#6C5BB9",
		DataResidency:  residencyJSON,
		ExternalKeyID:  normalizedOptionalString(req.ExternalKeyID),
		Tags:           tagsJSON,
	})
	if err != nil {
		return workspaceResponse{}, mapAdminDBError(err, "Could not create workspace")
	}
	return s.workspaceFromRecord(record), nil
}

func (s *Service) GetWorkspace(ctx context.Context, principal auth.Principal, workspaceID string) (workspaceResponse, error) {
	workspace, err := s.db.GetAdminWorkspace(ctx, principal.OrganizationID, workspaceID)
	if err != nil {
		return workspaceResponse{}, mapAdminDBError(err, "Workspace not found")
	}
	return s.workspaceFromRecord(workspace), nil
}

func (s *Service) ListWorkspaces(ctx context.Context, principal auth.Principal, includeArchived bool, afterID, beforeID string, limit int) (cursorPageResponse[workspaceResponse], error) {
	records, hasMore, err := s.db.ListAdminWorkspacesPage(ctx, db.ListAdminWorkspacesParams{
		OrganizationID:  principal.OrganizationID,
		IncludeArchived: includeArchived,
		AfterID:         afterID,
		BeforeID:        beforeID,
		Limit:           limit,
	})
	if err != nil {
		return cursorPageResponse[workspaceResponse]{}, err
	}
	data := make([]workspaceResponse, 0, len(records))
	for _, record := range records {
		data = append(data, s.workspaceFromRecord(record))
	}
	return cursorPage(data, hasMore, func(value workspaceResponse) string { return value.ID }), nil
}

func (s *Service) UpdateWorkspace(ctx context.Context, principal auth.Principal, workspaceID string, req updateWorkspaceRequest) (workspaceResponse, error) {
	current, err := s.db.GetAdminWorkspace(ctx, principal.OrganizationID, workspaceID)
	if err != nil {
		return workspaceResponse{}, mapAdminDBError(err, "Workspace not found")
	}
	currentResidency, err := decodeDataResidency(current.DataResidency)
	if err != nil {
		return workspaceResponse{}, invalidRequest("stored data_residency is invalid")
	}
	if req.DataResidency != nil && req.DataResidency.WorkspaceGeo != nil {
		requestedWorkspaceGeo := strings.TrimSpace(*req.DataResidency.WorkspaceGeo)
		if requestedWorkspaceGeo != "" && requestedWorkspaceGeo != currentResidency.WorkspaceGeo {
			return workspaceResponse{}, invalidRequest("workspace_geo is immutable")
		}
	}
	nextResidency, err := dataResidencyFromRequest(req.DataResidency, &currentResidency)
	if err != nil {
		return workspaceResponse{}, invalidRequest(err.Error())
	}
	nextResidency.WorkspaceGeo = currentResidency.WorkspaceGeo
	residencyJSON, err := encodeDataResidency(nextResidency)
	if err != nil {
		return workspaceResponse{}, err
	}
	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			return workspaceResponse{}, invalidRequest("name must be non-empty")
		}
	}
	tags := current.Tags
	if req.Tags != nil {
		if err := validateTags(req.Tags); err != nil {
			return workspaceResponse{}, invalidRequest(err.Error())
		}
		tags, err = json.Marshal(req.Tags)
		if err != nil {
			return workspaceResponse{}, err
		}
	}
	externalKeyID := current.ExternalKeyID
	if req.ExternalKeyID != nil {
		nextExternalKeyID := strings.TrimSpace(*req.ExternalKeyID)
		if nextExternalKeyID == "" {
			return workspaceResponse{}, invalidRequest("external_key_id cannot be empty")
		}
		if current.ExternalKeyID != nil && *current.ExternalKeyID != nextExternalKeyID {
			return workspaceResponse{}, conflict("external_key_id is write-once")
		}
		if _, err := s.db.GetAdminExternalKey(ctx, principal.OrganizationID, nextExternalKeyID); err != nil {
			return workspaceResponse{}, mapAdminDBError(err, "External key not found")
		}
		externalKeyID = &nextExternalKeyID
	}
	updated, err := s.db.UpdateAdminWorkspace(ctx, principal.OrganizationID, workspaceID, db.AdminWorkspace{
		Name:          name,
		DataResidency: residencyJSON,
		ExternalKeyID: externalKeyID,
		Tags:          tags,
		UpdatedAt:     time.Now().UTC(),
	})
	if err != nil {
		return workspaceResponse{}, mapAdminDBError(err, "Could not update workspace")
	}
	return s.workspaceFromRecord(updated), nil
}

func (s *Service) ArchiveWorkspace(ctx context.Context, principal auth.Principal, workspaceID string) (workspaceResponse, error) {
	workspace, err := s.db.ArchiveAdminWorkspace(ctx, principal.OrganizationID, workspaceID)
	if err != nil {
		return workspaceResponse{}, mapAdminDBError(err, "Workspace not found")
	}
	return s.workspaceFromRecord(workspace), nil
}

func (s *Service) CreateWorkspaceMember(ctx context.Context, principal auth.Principal, workspaceID string, req createWorkspaceMemberRequest) (workspaceMemberResponse, error) {
	if err := validateWorkspaceRole(req.WorkspaceRole, false); err != nil {
		return workspaceMemberResponse{}, invalidRequest(err.Error())
	}
	workspace, err := s.db.GetAdminWorkspace(ctx, principal.OrganizationID, workspaceID)
	if err != nil {
		return workspaceMemberResponse{}, mapAdminDBError(err, "Workspace not found")
	}
	user, err := s.db.GetAdminUser(ctx, principal.OrganizationID, req.UserID)
	if err != nil {
		return workspaceMemberResponse{}, mapAdminDBError(err, "User not found")
	}
	memberID, err := ids.New("wmem_")
	if err != nil {
		return workspaceMemberResponse{}, err
	}
	member, err := s.db.CreateAdminWorkspaceMember(ctx, db.AdminWorkspaceMember{
		ExternalID:          memberID,
		OrganizationID:      principal.OrganizationID,
		WorkspaceID:         workspace.ID,
		WorkspaceExternalID: workspace.ExternalID,
		UserID:              user.ID,
		UserExternalID:      user.ExternalID,
		WorkspaceRole:       req.WorkspaceRole,
		CreatedAt:           time.Now().UTC(),
	})
	if err != nil {
		return workspaceMemberResponse{}, mapAdminDBError(err, "Could not create workspace member")
	}
	return workspaceMemberFromRecord(member), nil
}

func (s *Service) GetWorkspaceMember(ctx context.Context, principal auth.Principal, workspaceID, userID string) (workspaceMemberResponse, error) {
	member, err := s.db.GetAdminWorkspaceMember(ctx, principal.OrganizationID, workspaceID, userID)
	if err != nil {
		return workspaceMemberResponse{}, mapAdminDBError(err, "Workspace member not found")
	}
	return workspaceMemberFromRecord(member), nil
}

func (s *Service) ListWorkspaceMembers(ctx context.Context, principal auth.Principal, workspaceID, afterID, beforeID string, limit int) (cursorPageResponse[workspaceMemberResponse], error) {
	workspace, err := s.db.GetAdminWorkspace(ctx, principal.OrganizationID, workspaceID)
	if err != nil {
		return cursorPageResponse[workspaceMemberResponse]{}, mapAdminDBError(err, "Workspace not found")
	}
	records, hasMore, err := s.db.ListAdminWorkspaceMembersPage(ctx, db.ListAdminMembersParams{
		OrganizationID: principal.OrganizationID,
		WorkspaceID:    workspace.ID,
		AfterID:        afterID,
		BeforeID:       beforeID,
		Limit:          limit,
	})
	if err != nil {
		return cursorPageResponse[workspaceMemberResponse]{}, err
	}
	data := make([]workspaceMemberResponse, 0, len(records))
	for _, record := range records {
		data = append(data, workspaceMemberFromRecord(record))
	}
	return cursorPage(data, hasMore, func(value workspaceMemberResponse) string { return value.UserID }), nil
}

func (s *Service) UpdateWorkspaceMember(ctx context.Context, principal auth.Principal, workspaceID, userID string, req updateWorkspaceMemberRequest) (workspaceMemberResponse, error) {
	if err := validateWorkspaceRole(req.WorkspaceRole, true); err != nil {
		return workspaceMemberResponse{}, invalidRequest(err.Error())
	}
	member, err := s.db.UpdateAdminWorkspaceMember(ctx, principal.OrganizationID, workspaceID, userID, req.WorkspaceRole)
	if err != nil {
		return workspaceMemberResponse{}, mapAdminDBError(err, "Workspace member not found")
	}
	return workspaceMemberFromRecord(member), nil
}

func (s *Service) DeleteWorkspaceMember(ctx context.Context, principal auth.Principal, workspaceID, userID string) (map[string]string, error) {
	member, err := s.db.DeleteAdminWorkspaceMember(ctx, principal.OrganizationID, workspaceID, userID)
	if err != nil {
		return nil, mapAdminDBError(err, "Workspace member not found")
	}
	return map[string]string{"type": "workspace_member_deleted", "user_id": member.UserExternalID, "workspace_id": member.WorkspaceExternalID}, nil
}

func (s *Service) GetAPIKey(ctx context.Context, principal auth.Principal, apiKeyID string) (apiKeyResponse, error) {
	key, err := s.db.GetAdminAPIKey(ctx, principal.OrganizationID, apiKeyID)
	if err != nil {
		return apiKeyResponse{}, mapAdminDBError(err, "API key not found")
	}
	return s.apiKeyFromRecord(key), nil
}

func (s *Service) ListAPIKeys(ctx context.Context, principal auth.Principal, workspaceID, createdByUserID, status, afterID, beforeID string, limit int) (cursorPageResponse[apiKeyResponse], error) {
	if status != "" {
		if err := validateAPIKeyStatus(status, true); err != nil {
			return cursorPageResponse[apiKeyResponse]{}, invalidRequest(err.Error())
		}
	}
	records, hasMore, err := s.db.ListAdminAPIKeysPage(ctx, db.ListAdminAPIKeysParams{
		OrganizationID:  principal.OrganizationID,
		WorkspaceID:     workspaceID,
		CreatedByUserID: createdByUserID,
		Status:          status,
		AfterID:         afterID,
		BeforeID:        beforeID,
		Limit:           limit,
	})
	if err != nil {
		return cursorPageResponse[apiKeyResponse]{}, err
	}
	data := make([]apiKeyResponse, 0, len(records))
	for _, record := range records {
		data = append(data, s.apiKeyFromRecord(record))
	}
	return cursorPage(data, hasMore, func(value apiKeyResponse) string { return value.ID }), nil
}

func (s *Service) UpdateAPIKey(ctx context.Context, principal auth.Principal, apiKeyID string, req updateAPIKeyRequest) (apiKeyResponse, error) {
	status := ""
	if req.Status != nil {
		status = strings.TrimSpace(*req.Status)
		if err := validateAPIKeyStatus(status, false); err != nil {
			return apiKeyResponse{}, invalidRequest(err.Error())
		}
	}
	name := ""
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			return apiKeyResponse{}, invalidRequest("name must be non-empty")
		}
	}
	key, err := s.db.UpdateAdminAPIKey(ctx, principal.OrganizationID, apiKeyID, req.Name != nil, name, req.Status != nil, status)
	if err != nil {
		return apiKeyResponse{}, mapAdminDBError(err, "API key not found")
	}
	return s.apiKeyFromRecord(key), nil
}

func (s *Service) CreateExternalKey(ctx context.Context, principal auth.Principal, req createExternalKeyRequest) (externalKeyResponse, error) {
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		return externalKeyResponse{}, invalidRequest("display_name is required")
	}
	geo := strings.TrimSpace(req.Geo)
	if geo == "" {
		geo = "us"
	}
	if geo != "us" {
		return externalKeyResponse{}, invalidRequest("geo must be us")
	}
	providerConfig, err := normalizeExternalKeyProviderConfig(req.ProviderConfig)
	if err != nil {
		return externalKeyResponse{}, invalidRequest(err.Error())
	}
	externalID, err := ids.New("ekey_")
	if err != nil {
		return externalKeyResponse{}, err
	}
	now := time.Now().UTC()
	key, err := s.db.CreateAdminExternalKey(ctx, db.AdminExternalKey{
		ExternalID:     externalID,
		OrganizationID: principal.OrganizationID,
		DisplayName:    displayName,
		Geo:            geo,
		ProviderConfig: providerConfig,
		CreatedAt:      now,
	})
	if err != nil {
		return externalKeyResponse{}, mapAdminDBError(err, "Could not create external key")
	}
	return externalKeyFromRecord(key), nil
}

func (s *Service) ListExternalKeys(ctx context.Context, principal auth.Principal, limit, offset int) (tokenPageResponse[externalKeyResponse], error) {
	records, hasMore, err := s.db.ListAdminExternalKeysPage(ctx, db.ListAdminOffsetParams{
		OrganizationID: principal.OrganizationID,
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		return tokenPageResponse[externalKeyResponse]{}, err
	}
	data := make([]externalKeyResponse, 0, len(records))
	for _, record := range records {
		data = append(data, externalKeyFromRecord(record))
	}
	return tokenPage(data, hasMore, offset), nil
}

func (s *Service) GetExternalKey(ctx context.Context, principal auth.Principal, externalKeyID string) (externalKeyResponse, error) {
	key, err := s.db.GetAdminExternalKey(ctx, principal.OrganizationID, externalKeyID)
	if err != nil {
		return externalKeyResponse{}, mapAdminDBError(err, "External key not found")
	}
	return externalKeyFromRecord(key), nil
}

func (s *Service) UpdateExternalKey(ctx context.Context, principal auth.Principal, externalKeyID string, req updateExternalKeyRequest) (externalKeyResponse, error) {
	current, err := s.db.GetAdminExternalKey(ctx, principal.OrganizationID, externalKeyID)
	if err != nil {
		return externalKeyResponse{}, mapAdminDBError(err, "External key not found")
	}
	next := current
	if req.DisplayName != nil {
		next.DisplayName = strings.TrimSpace(*req.DisplayName)
		if next.DisplayName == "" {
			return externalKeyResponse{}, invalidRequest("display_name must be non-empty")
		}
	}
	if req.Geo != nil {
		next.Geo = strings.TrimSpace(*req.Geo)
		if next.Geo != "us" {
			return externalKeyResponse{}, invalidRequest("geo must be us")
		}
	}
	if len(req.ProviderConfig) > 0 {
		normalized, err := normalizeExternalKeyProviderConfig(req.ProviderConfig)
		if err != nil {
			return externalKeyResponse{}, invalidRequest(err.Error())
		}
		next.ProviderConfig = normalized
	}
	refs, err := s.db.CountAdminExternalKeyWorkspaceRefs(ctx, principal.OrganizationID, externalKeyID)
	if err != nil {
		return externalKeyResponse{}, err
	}
	if refs > 0 && (next.Geo != current.Geo || !externalKeyProviderEqual(next.ProviderConfig, current.ProviderConfig)) {
		return externalKeyResponse{}, conflict("geo and provider_config cannot be changed while a workspace references this external key")
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := s.db.UpdateAdminExternalKey(ctx, principal.OrganizationID, externalKeyID, next)
	if err != nil {
		return externalKeyResponse{}, mapAdminDBError(err, "External key not found")
	}
	return externalKeyFromRecord(updated), nil
}

func (s *Service) DeleteExternalKey(ctx context.Context, principal auth.Principal, externalKeyID string) (map[string]string, error) {
	refs, err := s.db.CountAdminExternalKeyWorkspaceRefs(ctx, principal.OrganizationID, externalKeyID)
	if err != nil {
		return nil, err
	}
	if refs > 0 {
		return nil, conflict("external key is still referenced by a workspace")
	}
	if err := s.db.DeleteAdminExternalKey(ctx, principal.OrganizationID, externalKeyID); err != nil {
		return nil, mapAdminDBError(err, "External key not found")
	}
	return map[string]string{"id": externalKeyID, "type": "external_key_deleted"}, nil
}

func (s *Service) ValidateExternalKey(ctx context.Context, principal auth.Principal, externalKeyID string) (externalKeyValidationResponse, error) {
	if _, err := s.db.GetAdminExternalKey(ctx, principal.OrganizationID, externalKeyID); err != nil {
		return externalKeyValidationResponse{}, mapAdminDBError(err, "External key not found")
	}
	return externalKeyValidationResponse{Status: "success", Type: "external_key_validation"}, nil
}

func (s *Service) ListRateLimits(model, groupType string, workspace bool) (tokenPageResponse[rateLimitResponse], error) {
	if err := validateGroupType(groupType); err != nil {
		return tokenPageResponse[rateLimitResponse]{}, invalidRequest(err.Error())
	}
	if model != "" {
		return tokenPageResponse[rateLimitResponse]{}, notFound("Rate limit not found")
	}
	return tokenPageResponse[rateLimitResponse]{Data: []rateLimitResponse{}, NextPage: nil}, nil
}

func (s *Service) MessagesUsageReport(query reportQuery) (reportResponse, error) {
	if err := validateMessagesReportQuery(query); err != nil {
		return reportResponse{}, invalidRequest(err.Error())
	}
	return emptyReport(), nil
}

func (s *Service) ClaudeCodeUsageReport(query reportQuery) (reportResponse, error) {
	if err := validateClaudeCodeReportQuery(query); err != nil {
		return reportResponse{}, invalidRequest(err.Error())
	}
	return emptyReport(), nil
}

func (s *Service) CostReport(query reportQuery) (reportResponse, error) {
	if err := validateCostReportQuery(query); err != nil {
		return reportResponse{}, invalidRequest(err.Error())
	}
	return emptyReport(), nil
}

func (s *Service) GetTunnel(ctx context.Context, principal auth.Principal, tunnelID string) (tunnelResponse, error) {
	tunnel, err := s.db.GetAdminTunnel(ctx, principal.OrganizationID, tunnelID)
	if err != nil {
		return tunnelResponse{}, mapAdminDBError(err, "Tunnel not found")
	}
	return s.tunnelFromRecord(tunnel), nil
}

func (s *Service) ListTunnels(ctx context.Context, principal auth.Principal, workspaceID string, includeArchived bool, limit, offset int) (tokenPageResponse[tunnelResponse], error) {
	records, hasMore, err := s.db.ListAdminTunnelsPage(ctx, db.ListAdminTunnelsParams{
		OrganizationID:  principal.OrganizationID,
		WorkspaceID:     workspaceID,
		IncludeArchived: includeArchived,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		return tokenPageResponse[tunnelResponse]{}, err
	}
	data := make([]tunnelResponse, 0, len(records))
	for _, record := range records {
		data = append(data, s.tunnelFromRecord(record))
	}
	return tokenPage(data, hasMore, offset), nil
}

func (s *Service) RevealTunnelToken(ctx context.Context, principal auth.Principal, tunnelID string) (tunnelTokenResponse, error) {
	tunnel, err := s.db.GetAdminTunnel(ctx, principal.OrganizationID, tunnelID)
	if err != nil {
		return tunnelTokenResponse{}, mapAdminDBError(err, "Tunnel not found")
	}
	if tunnel.ArchivedAt != nil {
		return tunnelTokenResponse{}, conflict("Tunnel is archived")
	}
	if tunnel.TokenID != nil && tunnel.TunnelToken != nil {
		return tunnelTokenResponse{ID: *tunnel.TokenID, TunnelToken: *tunnel.TunnelToken, Type: "tunnel_token"}, nil
	}
	tokenID, token, err := newTunnelToken()
	if err != nil {
		return tunnelTokenResponse{}, err
	}
	updated, err := s.db.SetAdminTunnelToken(ctx, principal.OrganizationID, tunnelID, tokenID, token)
	if err != nil {
		return tunnelTokenResponse{}, mapAdminDBError(err, "Tunnel not found")
	}
	return tunnelTokenResponse{ID: *updated.TokenID, TunnelToken: *updated.TunnelToken, Type: "tunnel_token"}, nil
}

func (s *Service) RotateTunnelToken(ctx context.Context, principal auth.Principal, tunnelID string) (tunnelTokenResponse, error) {
	tunnel, err := s.db.GetAdminTunnel(ctx, principal.OrganizationID, tunnelID)
	if err != nil {
		return tunnelTokenResponse{}, mapAdminDBError(err, "Tunnel not found")
	}
	if tunnel.ArchivedAt != nil {
		return tunnelTokenResponse{}, conflict("Tunnel is archived")
	}
	tokenID, token, err := newTunnelToken()
	if err != nil {
		return tunnelTokenResponse{}, err
	}
	updated, err := s.db.SetAdminTunnelToken(ctx, principal.OrganizationID, tunnelID, tokenID, token)
	if err != nil {
		return tunnelTokenResponse{}, mapAdminDBError(err, "Tunnel not found")
	}
	return tunnelTokenResponse{ID: *updated.TokenID, TunnelToken: *updated.TunnelToken, Type: "tunnel_token"}, nil
}

func (s *Service) ArchiveTunnel(ctx context.Context, principal auth.Principal, tunnelID string) (tunnelResponse, error) {
	tunnel, err := s.db.ArchiveAdminTunnel(ctx, principal.OrganizationID, tunnelID)
	if err != nil {
		return tunnelResponse{}, mapAdminDBError(err, "Tunnel not found")
	}
	return s.tunnelFromRecord(tunnel), nil
}

func (s *Service) CreateTunnelCertificate(ctx context.Context, principal auth.Principal, tunnelID string, req createTunnelCertificateRequest) (tunnelCertificateResponse, error) {
	tunnel, err := s.db.GetAdminTunnel(ctx, principal.OrganizationID, tunnelID)
	if err != nil {
		return tunnelCertificateResponse{}, mapAdminDBError(err, "Tunnel not found")
	}
	if tunnel.ArchivedAt != nil {
		return tunnelCertificateResponse{}, conflict("Tunnel is archived")
	}
	count, err := s.db.CountActiveAdminTunnelCertificates(ctx, principal.OrganizationID, tunnel.ID)
	if err != nil {
		return tunnelCertificateResponse{}, err
	}
	if count >= 2 {
		return tunnelCertificateResponse{}, conflict("Tunnel already has two active certificates")
	}
	parsed, err := parseCACertificatePEM(req.CACertificatePEM)
	if err != nil {
		return tunnelCertificateResponse{}, invalidRequest(err.Error())
	}
	certID, err := ids.New("tcrt_")
	if err != nil {
		return tunnelCertificateResponse{}, err
	}
	cert, err := s.db.CreateAdminTunnelCertificate(ctx, db.AdminTunnelCertificate{
		ExternalID:       certID,
		OrganizationID:   principal.OrganizationID,
		TunnelID:         tunnel.ID,
		TunnelExternalID: tunnel.ExternalID,
		CACertificatePEM: req.CACertificatePEM,
		Fingerprint:      parsed.Fingerprint,
		ExpiresAt:        parsed.ExpiresAt,
		CreatedAt:        time.Now().UTC(),
	})
	if err != nil {
		return tunnelCertificateResponse{}, mapAdminDBError(err, "Could not create tunnel certificate")
	}
	return tunnelCertificateFromRecord(cert), nil
}

func (s *Service) GetTunnelCertificate(ctx context.Context, principal auth.Principal, tunnelID, certificateID string) (tunnelCertificateResponse, error) {
	cert, err := s.db.GetAdminTunnelCertificate(ctx, principal.OrganizationID, tunnelID, certificateID)
	if err != nil {
		return tunnelCertificateResponse{}, mapAdminDBError(err, "Tunnel certificate not found")
	}
	return tunnelCertificateFromRecord(cert), nil
}

func (s *Service) ListTunnelCertificates(ctx context.Context, principal auth.Principal, tunnelID string, includeArchived bool, limit, offset int) (tokenPageResponse[tunnelCertificateResponse], error) {
	tunnel, err := s.db.GetAdminTunnel(ctx, principal.OrganizationID, tunnelID)
	if err != nil {
		return tokenPageResponse[tunnelCertificateResponse]{}, mapAdminDBError(err, "Tunnel not found")
	}
	records, hasMore, err := s.db.ListAdminTunnelCertificatesPage(ctx, db.ListAdminTunnelCertificatesParams{
		OrganizationID:  principal.OrganizationID,
		TunnelID:        tunnel.ID,
		IncludeArchived: includeArchived,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		return tokenPageResponse[tunnelCertificateResponse]{}, err
	}
	data := make([]tunnelCertificateResponse, 0, len(records))
	for _, record := range records {
		data = append(data, tunnelCertificateFromRecord(record))
	}
	return tokenPage(data, hasMore, offset), nil
}

func (s *Service) ArchiveTunnelCertificate(ctx context.Context, principal auth.Principal, tunnelID, certificateID string) (tunnelCertificateResponse, error) {
	cert, err := s.db.ArchiveAdminTunnelCertificate(ctx, principal.OrganizationID, tunnelID, certificateID)
	if err != nil {
		return tunnelCertificateResponse{}, mapAdminDBError(err, "Tunnel certificate not found")
	}
	return tunnelCertificateFromRecord(cert), nil
}

func inviteFromRecord(record db.AdminInvite) inviteResponse {
	status := record.Status
	if status == "pending" && time.Now().UTC().After(record.ExpiresAt) {
		status = "expired"
	}
	return inviteResponse{
		ID:        record.ExternalID,
		Email:     record.Email,
		ExpiresAt: formatTime(record.ExpiresAt),
		InvitedAt: formatTime(record.InvitedAt),
		Role:      record.Role,
		Status:    status,
		Type:      "invite",
	}
}

func userFromRecord(record db.AdminUser) userResponse {
	return userResponse{
		ID:      record.ExternalID,
		AddedAt: formatTime(record.AddedAt),
		Email:   record.Email,
		Name:    record.Name,
		Role:    record.Role,
		Type:    "user",
	}
}

func (s *Service) workspaceFromRecord(record db.AdminWorkspace) workspaceResponse {
	residency, err := decodeDataResidency(record.DataResidency)
	if err != nil {
		residency = defaultDataResidency()
	}
	return workspaceResponse{
		ID:            record.ExternalID,
		ArchivedAt:    formatOptionalTime(record.ArchivedAt),
		CompartmentID: record.CompartmentID,
		CreatedAt:     formatTime(record.CreatedAt),
		DataResidency: residency,
		DisplayColor:  record.DisplayColor,
		ExternalKeyID: record.ExternalKeyID,
		Name:          record.Name,
		Tags:          tagsFromRaw(record.Tags),
		Type:          "workspace",
	}
}

func workspaceMemberFromRecord(record db.AdminWorkspaceMember) workspaceMemberResponse {
	return workspaceMemberResponse{
		Type:          "workspace_member",
		UserID:        record.UserExternalID,
		WorkspaceID:   record.WorkspaceExternalID,
		WorkspaceRole: record.WorkspaceRole,
	}
}

func (s *Service) apiKeyFromRecord(record db.AdminAPIKey) apiKeyResponse {
	createdBy := "user_default"
	if record.CreatedByUserExternalID != nil && *record.CreatedByUserExternalID != "" {
		createdBy = *record.CreatedByUserExternalID
	}
	name := record.Name
	if name == "" {
		name = record.ExternalID
	}
	hint := record.PartialKeyHint
	if hint == "" {
		hint = record.ExternalID
	}
	return apiKeyResponse{
		ID:             record.ExternalID,
		CreatedAt:      formatTime(record.CreatedAt),
		CreatedBy:      actorResponse{ID: createdBy, Type: "user"},
		ExpiresAt:      formatOptionalTime(record.ExpiresAt),
		Name:           name,
		PartialKeyHint: hint,
		Status:         effectiveAPIKeyStatus(record),
		Type:           "api_key",
		WorkspaceID:    s.nullableWorkspaceID(record.WorkspaceExternalID),
	}
}

func externalKeyFromRecord(record db.AdminExternalKey) externalKeyResponse {
	return externalKeyResponse{
		ID:             record.ExternalID,
		CreatedAt:      formatTime(record.CreatedAt),
		DisplayName:    record.DisplayName,
		Geo:            record.Geo,
		ProviderConfig: record.ProviderConfig,
		Type:           "external_key",
		UpdatedAt:      formatTime(record.UpdatedAt),
	}
}

func (s *Service) tunnelFromRecord(record db.AdminTunnel) tunnelResponse {
	return tunnelResponse{
		ID:          record.ExternalID,
		ArchivedAt:  formatOptionalTime(record.ArchivedAt),
		CreatedAt:   formatTime(record.CreatedAt),
		DisplayName: record.DisplayName,
		Domain:      record.Domain,
		Type:        "tunnel",
		WorkspaceID: nullableWorkspaceIDFromPtr(record.WorkspaceExternalID, s.cfg.Bootstrap.WorkspaceExternalID),
	}
}

func tunnelCertificateFromRecord(record db.AdminTunnelCertificate) tunnelCertificateResponse {
	return tunnelCertificateResponse{
		ID:          record.ExternalID,
		ArchivedAt:  formatOptionalTime(record.ArchivedAt),
		CreatedAt:   formatTime(record.CreatedAt),
		ExpiresAt:   formatOptionalTime(record.ExpiresAt),
		Fingerprint: record.Fingerprint,
		TunnelID:    record.TunnelExternalID,
		Type:        "tunnel_certificate",
	}
}

func mapAdminDBError(err error, missingMessage string) error {
	if errors.Is(err, db.ErrNotFound) {
		return notFound(missingMessage)
	}
	if errors.Is(err, db.ErrDuplicate) {
		return conflict("Resource already exists")
	}
	return err
}

func invalidRequest(message string) error {
	return &serviceError{status: http.StatusBadRequest, typ: "invalid_request_error", message: message}
}

func notFound(message string) error {
	return &serviceError{status: http.StatusNotFound, typ: "not_found_error", message: message}
}

func conflict(message string) error {
	return &serviceError{status: http.StatusConflict, typ: "conflict_error", message: message}
}

func emptyReport() reportResponse {
	return reportResponse{Data: []any{}, HasMore: false, NextPage: nil}
}

func defaultTags(tags map[string]string) map[string]string {
	if tags == nil {
		return map[string]string{}
	}
	return tags
}

func tagsFromRaw(raw []byte) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	var tags map[string]string
	if err := json.Unmarshal(raw, &tags); err != nil || tags == nil {
		return map[string]string{}
	}
	return tags
}

func normalizedOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTime(*value)
	return &formatted
}

func effectiveAPIKeyStatus(record db.AdminAPIKey) string {
	if record.ExpiresAt != nil && time.Now().UTC().After(*record.ExpiresAt) {
		return "expired"
	}
	return record.Status
}

func (s *Service) nullableWorkspaceID(workspaceID string) *string {
	if workspaceID == "" || workspaceID == s.cfg.Bootstrap.WorkspaceExternalID {
		return nil
	}
	return &workspaceID
}

func nullableWorkspaceIDFromPtr(workspaceID *string, defaultWorkspaceID string) *string {
	if workspaceID == nil || *workspaceID == "" || *workspaceID == defaultWorkspaceID {
		return nil
	}
	return workspaceID
}

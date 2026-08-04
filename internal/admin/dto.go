package admin

import "encoding/json"

type createInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type updateUserRequest struct {
	Role string `json:"role"`
}

type createWorkspaceRequest struct {
	Name          string            `json:"name"`
	ExternalKeyID *string           `json:"external_key_id"`
	Tags          map[string]string `json:"tags"`
}

type updateWorkspaceRequest struct {
	Name          *string           `json:"name"`
	ExternalKeyID *string           `json:"external_key_id"`
	Tags          map[string]string `json:"tags"`
}

type createWorkspaceMemberRequest struct {
	UserID        string `json:"user_id"`
	WorkspaceRole string `json:"workspace_role"`
}

type updateWorkspaceMemberRequest struct {
	WorkspaceRole string `json:"workspace_role"`
}

type updateAPIKeyRequest struct {
	Name   *string `json:"name"`
	Status *string `json:"status"`
}

type createExternalKeyRequest struct {
	DisplayName    string          `json:"display_name"`
	ProviderConfig json.RawMessage `json:"provider_config"`
	Geo            string          `json:"geo"`
}

type updateExternalKeyRequest struct {
	DisplayName    *string         `json:"display_name"`
	ProviderConfig json.RawMessage `json:"provider_config"`
	Geo            *string         `json:"geo"`
}

type rotateTunnelTokenRequest struct {
	Reason string `json:"reason"`
}

type createTunnelCertificateRequest struct {
	CACertificatePEM string `json:"ca_certificate_pem"`
}

type organizationResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type inviteResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	ExpiresAt string `json:"expires_at"`
	InvitedAt string `json:"invited_at"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	Type      string `json:"type"`
}

type userResponse struct {
	ID      string `json:"id"`
	AddedAt string `json:"added_at"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Type    string `json:"type"`
}

type workspaceResponse struct {
	ID            string            `json:"id"`
	ArchivedAt    *string           `json:"archived_at"`
	CompartmentID string            `json:"compartment_id"`
	CreatedAt     string            `json:"created_at"`
	DisplayColor  string            `json:"display_color"`
	ExternalKeyID *string           `json:"external_key_id"`
	Name          string            `json:"name"`
	Tags          map[string]string `json:"tags"`
	Type          string            `json:"type"`
}

type workspaceMemberResponse struct {
	Type          string `json:"type"`
	UserID        string `json:"user_id"`
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceRole string `json:"workspace_role"`
}

type actorResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type apiKeyResponse struct {
	ID             string        `json:"id"`
	CreatedAt      string        `json:"created_at"`
	CreatedBy      actorResponse `json:"created_by"`
	ExpiresAt      *string       `json:"expires_at"`
	Name           string        `json:"name"`
	PartialKeyHint string        `json:"partial_key_hint"`
	Status         string        `json:"status"`
	Type           string        `json:"type"`
	WorkspaceID    *string       `json:"workspace_id"`
}

type externalKeyResponse struct {
	ID             string          `json:"id"`
	CreatedAt      string          `json:"created_at"`
	DisplayName    string          `json:"display_name"`
	Geo            string          `json:"geo"`
	ProviderConfig json.RawMessage `json:"provider_config"`
	Type           string          `json:"type"`
	UpdatedAt      string          `json:"updated_at"`
}

type externalKeyValidationResponse struct {
	Error  *string `json:"error"`
	Status string  `json:"status"`
	Type   string  `json:"type"`
}

type rateLimitResponse struct {
	GroupType string          `json:"group_type"`
	Limits    []rateLimitItem `json:"limits"`
	Models    []string        `json:"models"`
	Type      string          `json:"type"`
}

type rateLimitItem struct {
	OrgLimit *int64 `json:"org_limit,omitempty"`
	Type     string `json:"type"`
	Value    int64  `json:"value"`
}

type tunnelResponse struct {
	ID          string  `json:"id"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
	DisplayName *string `json:"display_name"`
	Domain      string  `json:"domain"`
	Type        string  `json:"type"`
	WorkspaceID *string `json:"workspace_id"`
}

type tunnelTokenResponse struct {
	ID          string `json:"id"`
	TunnelToken string `json:"tunnel_token"`
	Type        string `json:"type"`
}

type tunnelCertificateResponse struct {
	ID          string  `json:"id"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   *string `json:"expires_at"`
	Fingerprint string  `json:"fingerprint"`
	TunnelID    string  `json:"tunnel_id"`
	Type        string  `json:"type"`
}

type cursorPageResponse[T any] struct {
	Data    []T     `json:"data"`
	FirstID *string `json:"first_id"`
	HasMore bool    `json:"has_more"`
	LastID  *string `json:"last_id"`
}

type tokenPageResponse[T any] struct {
	Data     []T     `json:"data"`
	NextPage *string `json:"next_page"`
}

type reportResponse struct {
	Data     []any   `json:"data"`
	HasMore  bool    `json:"has_more"`
	NextPage *string `json:"next_page"`
}

type reportQuery struct {
	StartingAt  string
	EndingAt    string
	BucketWidth string
	Limit       int
	Page        string
}

package platform

import "time"

type AdminRequest struct {
	UUID              string
	OrgUUID           string
	RequestType       string
	RequesterUUID     *string
	RequestedSeatTier *string
	Details           map[string]any
	Status            string
	CreatedAt         time.Time
	ResolvedAt        *time.Time
	RequesterEmail    *string
	RequesterName     *string
	RequesterRole     *string
	RequesterSeatTier *string
}

type ConsoleWorkspace struct {
	UUID                  string
	OrgUUID               string
	Name                  string
	DisplayColor          string
	Color                 string
	DataResidency         *string
	DataResidencySettings *ConsoleWorkspaceDataResidency
	ExternalKeyID         *string
	ExternalMapping       map[string]any
	Tags                  map[string]string
	ArchivedAt            *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type ConsoleWorkspaceDataResidency struct {
	WorkspaceGeo         string
	AllowedInferenceGeos string
	DefaultInferenceGeo  string
}

type CreateConsoleWorkspaceInput struct {
	OrgUUID       string
	Name          string
	DisplayColor  string
	Color         string
	DataResidency *string
}

type ConsoleAPIKey struct {
	ID                string
	OrgUUID           string
	WorkspaceID       string
	Name              string
	KeyPrefix         string
	KeySuffix         string
	Status            string
	CreatedByUserUUID *string
	LastUsedAt        *time.Time
	ExpiresAt         *time.Time
	ArchivedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateConsoleAPIKeyInput struct {
	OrgUUID           string
	WorkspaceID       string
	Name              string
	ExpiresAt         *time.Time
	CreatedByUserUUID *string
}

type CreateConsoleAPIKeyResult struct {
	APIKey ConsoleAPIKey
	RawKey string
}

type UpdateConsoleAPIKeyStatusInput struct {
	OrgUUID     string
	WorkspaceID string
	APIKeyID    string
	Status      string
}

type ConsoleInvite struct {
	ID        string
	Email     string
	Role      string
	Status    string
	InvitedAt time.Time
	ExpiresAt time.Time
}

type CreateConsoleInviteInput struct {
	OrgUUID string
	Email   string
	Role    string
}

type OrgUser struct {
	UserUUID string
	Email    string
	FullName *string
	Role     string
	AddedAt  time.Time
}

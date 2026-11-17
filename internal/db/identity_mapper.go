package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper IdentityMapper -sql ./identity_mapper.xml -out ./identity_mapper.sqlmap.gen.go -dialect postgres

type IdentityRecord struct {
	UUID              string     `db:"uuid"`
	ExternalID        string     `db:"external_id"`
	OrganizationUUID  string     `db:"organization_uuid"`
	WorkspaceUUID     string     `db:"workspace_uuid"`
	Namespace         string     `db:"namespace"`
	ExternalSubjectID string     `db:"external_subject_id"`
	DisplayName       string     `db:"display_name"`
	Enabled           bool       `db:"enabled"`
	AutoMemoryStore   bool       `db:"auto_memory_store"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
	DeletedAt         *time.Time `db:"deleted_at"`
}

type InsertIdentityParams struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	Namespace         string
	ExternalSubjectID string
	DisplayName       string
	Enabled           bool
	AutoMemoryStore   bool
}

type UpdateIdentityParams struct {
	WorkspaceUUID     string
	ExternalID        string
	Namespace         string
	ExternalSubjectID string
	DisplayName       string
	AutoMemoryStore   bool
}

type ListIdentityParams struct {
	WorkspaceUUID string
	Cursor        string
	Status        string
	Subject       string
	Namespace     string
	Search        string
	Limit         int
}

type IdentityConfigRecord struct {
	UUID             string    `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID string    `db:"organization_uuid"`
	WorkspaceUUID    string    `db:"workspace_uuid"`
	IdentityUUID     string    `db:"identity_uuid"`
	AgentExternalID  string    `db:"agent_external_id"`
	Revision         int64     `db:"revision"`
	Status           string    `db:"status"`
	Env              string    `db:"env"`
	VaultMounts      string    `db:"vault_mounts"`
	DisabledVaults   string    `db:"disabled_vaults"`
	CreatedAt        time.Time `db:"created_at"`
	UpdatedAt        time.Time `db:"updated_at"`
}

type InsertIdentityConfigParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	IdentityUUID     string
	AgentExternalID  string
	Env              string
	VaultMounts      string
	DisabledVaults   string
}

type UpdateIdentityConfigParams struct {
	WorkspaceUUID    string
	ExternalID       string
	ExpectedRevision int64
	Env              string
	VaultMounts      string
	DisabledVaults   string
}

type ExecutionContextRecord struct {
	UUID              string    `db:"uuid"`
	OrganizationUUID  string    `db:"organization_uuid"`
	WorkspaceUUID     string    `db:"workspace_uuid"`
	ChannelUUID       string    `db:"channel_uuid"`
	ChannelExternalID string    `db:"channel_external_id"`
	Mode              string    `db:"mode"`
	IdentityUUID      *string   `db:"identity_uuid"`
	AgentVersion      *int      `db:"agent_version"`
	Revision          int64     `db:"revision"`
	ConnectionStatus  string    `db:"connection_status"`
	EntryClosed       bool      `db:"entry_closed"`
	UpdatedAt         time.Time `db:"updated_at"`
}

type InsertExecutionContextParams struct {
	UUID              string
	OrganizationUUID  string
	WorkspaceUUID     string
	ChannelUUID       string
	ChannelExternalID string
	Mode              string
	IdentityUUID      *string
	AgentVersion      *int
}

type UpdateExecutionTargetParams struct {
	WorkspaceUUID string
	ChannelUUID   string
	Mode          string
	IdentityUUID  *string
	AgentVersion  *int
	Revision      int64
}

type ScopePairingRecord struct {
	UUID              string     `db:"uuid"`
	ExternalID        string     `db:"external_id"`
	OrganizationUUID  string     `db:"organization_uuid"`
	WorkspaceUUID     string     `db:"workspace_uuid"`
	ChannelUUID       string     `db:"channel_uuid"`
	ChannelExternalID string     `db:"channel_external_id"`
	ScopeKind         string     `db:"scope_kind"`
	PlatformAccount   string     `db:"platform_account"`
	RemoteID          string     `db:"remote_id"`
	DisplayName       string     `db:"display_name"`
	Status            string     `db:"status"`
	IdentityUUID      *string    `db:"identity_uuid"`
	AgentExternalID   string     `db:"agent_external_id"`
	AgentVersion      *int       `db:"agent_version"`
	Revision          int64      `db:"revision"`
	CodeExpiresAt     *time.Time `db:"pairing_code_expires_at"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
}

type InsertScopePairingParams struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	ChannelUUID       string
	ChannelExternalID string
	ScopeKind         string
	PlatformAccount   string
	RemoteID          string
	DisplayName       string
	CodeHash          string
	CodeExpiresAt     time.Time
}

type ActivateScopePairingParams struct {
	WorkspaceUUID   string
	ExternalID      string
	IdentityUUID    string
	AgentExternalID string
	AgentVersion    *int
	CodeHash        string
	Now             time.Time
}

type RebindScopePairingParams struct {
	WorkspaceUUID    string
	ExternalID       string
	IdentityUUID     string
	AgentExternalID  string
	AgentVersion     *int
	ExpectedRevision int64
}

type ListScopePairingParams struct {
	WorkspaceUUID     string
	ChannelExternalID string
	Status            string
	Cursor            string
	Limit             int
}

type SessionIdentityRecord struct {
	SessionUUID       string    `db:"session_uuid"`
	SessionExternalID string    `db:"session_external_id"`
	IdentityUUID      string    `db:"identity_uuid"`
	AgentExternalID   string    `db:"agent_external_id"`
	BindingRevision   int64     `db:"binding_revision"`
	ConfigRevision    int64     `db:"config_revision"`
	PersonalVaultID   string    `db:"personal_vault_external_id"`
	ScopeKind         string    `db:"scope_kind"`
	ScopeKey          string    `db:"scope_key"`
	ChannelExternalID string    `db:"channel_external_id"`
	SessionStatus     string    `db:"session_status"`
	CreatedAt         time.Time `db:"created_at"`
}

type InsertSessionIdentityParams struct {
	UUID              string
	OrganizationUUID  string
	WorkspaceUUID     string
	SessionUUID       string
	SessionExternalID string
	IdentityUUID      string
	AgentExternalID   string
	BindingRevision   int64
	ConfigRevision    int64
	PersonalVaultID   string
	ScopeKind         string
	ScopeKey          string
	ChannelExternalID string
}

type IdentityStatsRecord struct {
	Sessions   int        `db:"sessions"`
	Agents     int        `db:"agents"`
	LastUsedAt *time.Time `db:"last_used_at"`
}

type IdentityAgentUsageRecord struct {
	AgentExternalID string     `db:"agent_external_id"`
	Sessions        int        `db:"sessions"`
	LastUsedAt      *time.Time `db:"last_used_at"`
}

type AgentBindingRecord struct {
	IdentityExternalID string `db:"identity_external_id"`
	DisplayName        string `db:"display_name"`
	ChannelExternalID  string `db:"channel_external_id"`
	ScopeKind          string `db:"scope_kind"`
	RemoteID           string `db:"remote_id"`
	PairingExternalID  string `db:"pairing_external_id"`
	Status             string `db:"status"`
}

type CleanupRunRecord struct {
	ExternalID string    `db:"external_id"`
	State      string    `db:"state"`
	Result     string    `db:"result"`
	UpdatedAt  time.Time `db:"updated_at"`
}

type InsertCleanupRunParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	IdentityUUID     string
	State            string
	Result           string
}

type IdentityMapper interface {
	InsertIdentity(ctx context.Context, params InsertIdentityParams) (IdentityRecord, error)
	FindIdentity(ctx context.Context, workspaceUUID, externalID string) (IdentityRecord, bool, error)
	FindIdentityByUUID(ctx context.Context, workspaceUUID, identityUUID string) (IdentityRecord, bool, error)
	ListIdentities(ctx context.Context, params ListIdentityParams) ([]IdentityRecord, error)
	UpdateIdentity(ctx context.Context, params UpdateIdentityParams) (int64, error)
	SetIdentityEnabled(ctx context.Context, workspaceUUID, externalID string, enabled bool) (int64, error)
	SoftDeleteIdentity(ctx context.Context, workspaceUUID, externalID string) (int64, error)
	InsertIdentityConfig(ctx context.Context, params InsertIdentityConfigParams) (IdentityConfigRecord, error)
	FindActiveIdentityConfig(ctx context.Context, workspaceUUID, identityUUID, agentExternalID string) (IdentityConfigRecord, bool, error)
	ListIdentityConfigs(ctx context.Context, workspaceUUID, identityUUID, agentExternalID, cursor string, limit int) ([]IdentityConfigRecord, error)
	UpdateIdentityConfig(ctx context.Context, params UpdateIdentityConfigParams) (int64, error)
	ArchiveIdentityConfigs(ctx context.Context, workspaceUUID, identityUUID string) (int64, error)
	InsertExecutionContext(ctx context.Context, params InsertExecutionContextParams) (ExecutionContextRecord, error)
	FindExecutionContext(ctx context.Context, workspaceUUID, channelExternalID string) (ExecutionContextRecord, bool, error)
	UpdateExecutionTarget(ctx context.Context, params UpdateExecutionTargetParams) (int64, error)
	SetConnectionStatus(ctx context.Context, workspaceUUID, channelExternalID, status string) (int64, error)
	CloseFixedEntries(ctx context.Context, workspaceUUID, identityUUID string) (int64, error)
	InsertScopePairing(ctx context.Context, params InsertScopePairingParams) (ScopePairingRecord, error)
	FindScopePairing(ctx context.Context, workspaceUUID, externalID string) (ScopePairingRecord, bool, error)
	FindOpenScopePairing(ctx context.Context, channelUUID, scopeKind, platformAccount, remoteID string) (ScopePairingRecord, bool, error)
	ListScopePairings(ctx context.Context, params ListScopePairingParams) ([]ScopePairingRecord, error)
	ActivateScopePairingByCode(ctx context.Context, params ActivateScopePairingParams) (int64, error)
	ActivateScopePairingByID(ctx context.Context, params ActivateScopePairingParams) (int64, error)
	RebindScopePairing(ctx context.Context, params RebindScopePairingParams) (int64, error)
	UnbindScopePairing(ctx context.Context, workspaceUUID, externalID string, expectedRevision int64) (int64, error)
	UnbindIdentityPairings(ctx context.Context, workspaceUUID, identityUUID string) (int64, error)
	UpsertActorLink(ctx context.Context, organizationUUID, workspaceUUID, channelType, appID, actorID, identityUUID string) error
	SetSessionIdentity(ctx context.Context, workspaceUUID, sessionExternalID, identityUUID string) (int64, error)
	InsertSessionIdentity(ctx context.Context, params InsertSessionIdentityParams) error
	FindSessionIdentity(ctx context.Context, workspaceUUID, sessionExternalID string) (SessionIdentityRecord, bool, error)
	ListIdentitySessions(ctx context.Context, workspaceUUID, identityUUID string, limit int) ([]SessionIdentityRecord, error)
	IdentityStats(ctx context.Context, workspaceUUID, identityUUID string, since *time.Time) (IdentityStatsRecord, error)
	ListIdentityAgents(ctx context.Context, workspaceUUID, identityUUID string, since *time.Time, limit int) ([]IdentityAgentUsageRecord, error)
	ListRunningIdentitySessions(ctx context.Context, workspaceUUID, identityUUID string) ([]string, error)
	ListAgentBindings(ctx context.Context, workspaceUUID, agentExternalID string) ([]AgentBindingRecord, error)
	PersonalVault(ctx context.Context, workspaceUUID, vaultExternalID string) (bool, error)
	InsertCleanupRun(ctx context.Context, params InsertCleanupRunParams) (CleanupRunRecord, error)
	SaveCleanupRun(ctx context.Context, workspaceUUID, externalID, state, result string) (int64, error)
	FindCleanupRun(ctx context.Context, workspaceUUID, externalID string) (CleanupRunRecord, bool, error)
}

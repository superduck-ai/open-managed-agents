package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ChannelRecordMapper -sql ./channel_records_mapper.xml -out ./channel_records_mapper.sqlmap.gen.go -dialect postgres

type ChannelRecord struct {
	UUID              string     `db:"uuid"`
	ExternalID        string     `db:"external_id"`
	OrganizationUUID  string     `db:"organization_uuid"`
	WorkspaceUUID     string     `db:"workspace_uuid"`
	AgentExternalID   string     `db:"agent_external_id"`
	Name              string     `db:"name"`
	ChannelType       string     `db:"channel_type"`
	Mode              string     `db:"mode"`
	Enabled           bool       `db:"enabled"`
	BindingStatus     string     `db:"binding_status"`
	AppID             string     `db:"app_id"`
	AppSecretEnvelope *string    `db:"app_secret_envelope"`
	IncludeThinking   bool       `db:"include_thinking"`
	IncludeToolCalls  bool       `db:"include_tool_calls"`
	ReplyInThread     bool       `db:"reply_in_thread"`
	Mounts            string     `db:"mounts"`
	GitTokenEnvelope  *string    `db:"git_token_envelope"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
	DeletedAt         *time.Time `db:"deleted_at"`
}

type ChannelPairingRecord struct {
	ExternalID        string    `db:"external_id"`
	ChannelExternalID string    `db:"channel_external_id"`
	Kind              string    `db:"kind"`
	Target            string    `db:"target"`
	DisplayName       string    `db:"display_name"`
	Status            string    `db:"status"`
	CreatedAt         time.Time `db:"created_at"`
}

type insertChannelRecordParams struct {
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	AgentExternalID   string
	Name              string
	ChannelType       string
	Mode              string
	Enabled           bool
	BindingStatus     string
	AppID             string
	AppSecretEnvelope *string
	IncludeThinking   bool
	IncludeToolCalls  bool
	ReplyInThread     bool
	Mounts            string
	GitTokenEnvelope  *string
}

type updateChannelRecordParams struct {
	ExternalID        string
	WorkspaceUUID     string
	Name              string
	Enabled           bool
	BindingStatus     string
	AppID             string
	AppSecretEnvelope *string
	IncludeThinking   bool
	IncludeToolCalls  bool
	ReplyInThread     bool
	Mounts            string
	GitTokenEnvelope  *string
}

type insertChannelPairingParams struct {
	ExternalID        string
	ChannelExternalID string
	Kind              string
	Target            string
	DisplayName       string
}

type ChannelRecordMapper interface {
	LockAgent(ctx context.Context, workspaceUUID, agentExternalID string) (string, error)
	InsertChannel(ctx context.Context, params insertChannelRecordParams) (ChannelRecord, error)
	FindChannel(ctx context.Context, organizationUUID, workspaceUUID, agentExternalID, externalID string) (ChannelRecord, error)
	FindChannelByID(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (ChannelRecord, error)
	ListChannels(ctx context.Context, organizationUUID, workspaceUUID, agentExternalID string) ([]ChannelRecord, error)
	ListWorkspaceChannels(ctx context.Context, organizationUUID, workspaceUUID string) ([]ChannelRecord, error)
	ListRunnableChannels(ctx context.Context) ([]ChannelRecord, error)
	UpdateChannel(ctx context.Context, params updateChannelRecordParams) (ChannelRecord, error)
	SoftDeleteChannel(ctx context.Context, workspaceUUID, externalID string) error
	ListPairings(ctx context.Context, channelExternalID string) ([]ChannelPairingRecord, error)
	CountPairings(ctx context.Context, channelExternalID string) (int, error)
	InsertPairing(ctx context.Context, params insertChannelPairingParams) error
	UpdatePairingStatus(ctx context.Context, channelExternalID, pairingExternalID, status string) error
	FindChannelSession(ctx context.Context, channelExternalID, scopeKey string) (string, error)
	UpsertChannelSession(ctx context.Context, channelExternalID, scopeKey, sessionExternalID string) error
	FindChannelAgentEnvironment(ctx context.Context, organizationUUID, workspaceUUID, agentExternalID string) (string, error)
	UpsertChannelAgentEnvironment(ctx context.Context, organizationUUID, workspaceUUID, agentExternalID, environmentExternalID string) error
	ArchiveChannelSessions(ctx context.Context, channelExternalID string) error
	DeleteChannelSession(ctx context.Context, channelExternalID, scopeKey string) error
}

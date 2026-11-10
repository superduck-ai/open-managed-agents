package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper ChannelCredentialMapper -sql ./channel_credentials_mapper.xml -out ./channel_credentials_mapper.sqlmap.gen.go -dialect postgres

type channelCredentialRow struct {
	ExternalID string `db:"external_id"`
	Envelope   string `db:"envelope"`
	Status     string `db:"status"`
}

type insertChannelCredentialParams struct {
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	APIKeyUUID       string
	Envelope         string
}

type ChannelCredentialMapper interface {
	LockWorkspace(ctx context.Context, organizationUUID, workspaceUUID string) (string, error)
	Find(ctx context.Context, organizationUUID, workspaceUUID string) (channelCredentialRow, bool, error)
	Insert(ctx context.Context, params insertChannelCredentialParams) error
}

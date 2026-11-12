package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ChannelRegistrationMapper -sql ./channel_registrations_mapper.xml -out ./channel_registrations_mapper.sqlmap.gen.go -dialect postgres

type ChannelRegistrationRecord struct {
	ExternalID         string    `db:"external_id"`
	Kind               string    `db:"kind"`
	OrganizationUUID   string    `db:"organization_uuid"`
	WorkspaceUUID      string    `db:"workspace_uuid"`
	ChannelExternalID  string    `db:"channel_external_id"`
	Name               string    `db:"name"`
	Phase              string    `db:"phase"`
	URL                string    `db:"url"`
	ExpiresAt          time.Time `db:"expires_at"`
	NextPollAt         time.Time `db:"next_poll_at"`
	IntervalSeconds    int       `db:"interval_seconds"`
	Domain             string    `db:"domain"`
	Owner              string    `db:"owner"`
	HeartbeatAt        time.Time `db:"heartbeat_at"`
	DeviceCodeEnvelope *string   `db:"device_code_envelope"`
	AppID              string    `db:"app_id"`
	AppSecretEnvelope  *string   `db:"app_secret_envelope"`
	CreatedAt          time.Time `db:"created_at"`
	UpdatedAt          time.Time `db:"updated_at"`
}

type ChannelRegistrationInput struct {
	ExternalID         string
	Kind               string
	OrganizationUUID   string
	WorkspaceUUID      string
	ChannelExternalID  string
	Name               string
	Phase              string
	URL                string
	ExpiresAt          time.Time
	NextPollAt         time.Time
	IntervalSeconds    int
	Domain             string
	Owner              string
	DeviceCodeEnvelope *string
	AppID              string
	AppSecretEnvelope  *string
}

type ChannelRegistrationUpdate struct {
	ExternalID         string
	Phase              string
	URL                string
	ExpiresAt          time.Time
	NextPollAt         time.Time
	IntervalSeconds    int
	Domain             string
	Owner              string
	DeviceCodeEnvelope *string
	AppID              string
	AppSecretEnvelope  *string
}

type ChannelRegistrationMapper interface {
	InsertRegistration(ctx context.Context, params insertChannelRegistrationParams) (ChannelRegistrationRecord, error)
	FindRegistration(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (ChannelRegistrationRecord, error)
	FindChannelQrRegistration(ctx context.Context, channelExternalID string) (ChannelRegistrationRecord, error)
	UpdateRegistration(ctx context.Context, params updateChannelRegistrationParams) (ChannelRegistrationRecord, error)
	TransitionRegistration(ctx context.Context, params updateChannelRegistrationParams) (ChannelRegistrationRecord, error)
	DeleteRegistration(ctx context.Context, externalID string) error
}

type insertChannelRegistrationParams struct {
	Owner              string
	ExternalID         string
	Kind               string
	OrganizationUUID   string
	WorkspaceUUID      string
	ChannelExternalID  string
	Name               string
	Phase              string
	URL                string
	ExpiresAt          time.Time
	NextPollAt         time.Time
	IntervalSeconds    int
	Domain             string
	DeviceCodeEnvelope *string
	AppID              string
	AppSecretEnvelope  *string
}

type updateChannelRegistrationParams struct {
	Owner              string
	ExternalID         string
	Phase              string
	URL                string
	ExpiresAt          time.Time
	NextPollAt         time.Time
	IntervalSeconds    int
	Domain             string
	DeviceCodeEnvelope *string
	AppID              string
	AppSecretEnvelope  *string
}

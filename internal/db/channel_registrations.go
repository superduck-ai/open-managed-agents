package db

import "context"

func (d *DB) CreateChannelRegistration(ctx context.Context, params ChannelRegistrationInput) (ChannelRegistrationRecord, error) {
	record, err := NewChannelRegistrationMapper(d.mapperDB).InsertRegistration(ctx, insertChannelRegistrationParams{
		ExternalID:         params.ExternalID,
		Kind:               params.Kind,
		OrganizationUUID:   params.OrganizationUUID,
		WorkspaceUUID:      params.WorkspaceUUID,
		ChannelExternalID:  params.ChannelExternalID,
		Name:               params.Name,
		Phase:              params.Phase,
		URL:                params.URL,
		ExpiresAt:          params.ExpiresAt,
		NextPollAt:         params.NextPollAt,
		IntervalSeconds:    params.IntervalSeconds,
		Domain:             params.Domain,
		DeviceCodeEnvelope: params.DeviceCodeEnvelope,
		AppID:              params.AppID,
		AppSecretEnvelope:  params.AppSecretEnvelope,
	})
	return record, mapNoRows(err)
}

func (d *DB) GetChannelRegistration(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (ChannelRegistrationRecord, error) {
	record, err := NewChannelRegistrationMapper(d.mapperDB).FindRegistration(ctx, organizationUUID, workspaceUUID, externalID)
	return record, mapNoRows(err)
}

func (d *DB) GetChannelQrRegistration(ctx context.Context, channelExternalID string) (ChannelRegistrationRecord, error) {
	record, err := NewChannelRegistrationMapper(d.mapperDB).FindChannelQrRegistration(ctx, channelExternalID)
	return record, mapNoRows(err)
}

func (d *DB) UpdateChannelRegistration(ctx context.Context, params ChannelRegistrationUpdate) (ChannelRegistrationRecord, error) {
	record, err := NewChannelRegistrationMapper(d.mapperDB).UpdateRegistration(ctx, updateChannelRegistrationParams{
		ExternalID:         params.ExternalID,
		Phase:              params.Phase,
		URL:                params.URL,
		ExpiresAt:          params.ExpiresAt,
		NextPollAt:         params.NextPollAt,
		IntervalSeconds:    params.IntervalSeconds,
		Domain:             params.Domain,
		DeviceCodeEnvelope: params.DeviceCodeEnvelope,
		AppID:              params.AppID,
		AppSecretEnvelope:  params.AppSecretEnvelope,
	})
	return record, mapNoRows(err)
}

func (d *DB) DeleteChannelRegistration(ctx context.Context, externalID string) error {
	return NewChannelRegistrationMapper(d.mapperDB).DeleteRegistration(ctx, externalID)
}

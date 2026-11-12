package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

func (d *DB) CreateChannelRecord(ctx context.Context, params ChannelRecordInput) (ChannelRecord, error) {
	var record ChannelRecord
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewChannelRecordMapper(executor)
		if _, err := mapper.LockAgent(ctx, params.WorkspaceUUID, params.AgentExternalID); err != nil {
			return mapNoRows(err)
		}
		inserted, err := mapper.InsertChannel(ctx, insertChannelRecordParams{
			ExternalID:        params.ExternalID,
			OrganizationUUID:  params.OrganizationUUID,
			WorkspaceUUID:     params.WorkspaceUUID,
			AgentExternalID:   params.AgentExternalID,
			Name:              params.Name,
			ChannelType:       params.ChannelType,
			Mode:              params.Mode,
			Enabled:           params.Enabled,
			BindingStatus:     params.BindingStatus,
			AppID:             params.AppID,
			AppSecretEnvelope: params.AppSecretEnvelope,
			IncludeThinking:   params.IncludeThinking,
			IncludeToolCalls:  params.IncludeToolCalls,
			ReplyInThread:     params.ReplyInThread,
			Mounts:            params.Mounts,
			GitTokenEnvelope:  params.GitTokenEnvelope,
		})
		record = inserted
		return err
	})
	return record, err
}

func (d *DB) GetChannelRecord(ctx context.Context, organizationUUID, workspaceUUID, agentExternalID, externalID string) (ChannelRecord, error) {
	record, err := NewChannelRecordMapper(d.mapperDB).FindChannel(ctx, organizationUUID, workspaceUUID, agentExternalID, externalID)
	return record, mapNoRows(err)
}

func (d *DB) GetChannelRecordByID(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (ChannelRecord, error) {
	record, err := NewChannelRecordMapper(d.mapperDB).FindChannelByID(ctx, organizationUUID, workspaceUUID, externalID)
	return record, mapNoRows(err)
}

func (d *DB) ListAgentChannelRecords(ctx context.Context, organizationUUID, workspaceUUID, agentExternalID string) ([]ChannelRecord, error) {
	return NewChannelRecordMapper(d.mapperDB).ListChannels(ctx, organizationUUID, workspaceUUID, agentExternalID)
}

func (d *DB) ListWorkspaceChannelRecords(ctx context.Context, organizationUUID, workspaceUUID string) ([]ChannelRecord, error) {
	return NewChannelRecordMapper(d.mapperDB).ListWorkspaceChannels(ctx, organizationUUID, workspaceUUID)
}

func (d *DB) ListRunnableChannelRecords(ctx context.Context) ([]ChannelRecord, error) {
	return NewChannelRecordMapper(d.mapperDB).ListRunnableChannels(ctx)
}

func (d *DB) UpdateChannelRecord(ctx context.Context, params ChannelRecordUpdate) (ChannelRecord, error) {
	record, err := NewChannelRecordMapper(d.mapperDB).UpdateChannel(ctx, updateChannelRecordParams{
		ExternalID:        params.ExternalID,
		WorkspaceUUID:     params.WorkspaceUUID,
		Name:              params.Name,
		Enabled:           params.Enabled,
		BindingStatus:     params.BindingStatus,
		AppID:             params.AppID,
		AppSecretEnvelope: params.AppSecretEnvelope,
		IncludeThinking:   params.IncludeThinking,
		IncludeToolCalls:  params.IncludeToolCalls,
		ReplyInThread:     params.ReplyInThread,
		Mounts:            params.Mounts,
		GitTokenEnvelope:  params.GitTokenEnvelope,
	})
	return record, mapNoRows(err)
}

func (d *DB) SoftDeleteChannelRecord(ctx context.Context, workspaceUUID, externalID string) error {
	return NewChannelRecordMapper(d.mapperDB).SoftDeleteChannel(ctx, workspaceUUID, externalID)
}

func (d *DB) ListChannelPairingRecords(ctx context.Context, channelExternalID string) ([]ChannelPairingRecord, error) {
	return NewChannelRecordMapper(d.mapperDB).ListPairings(ctx, channelExternalID)
}

func (d *DB) CountChannelPairingRecords(ctx context.Context, channelExternalID string) (int, error) {
	return NewChannelRecordMapper(d.mapperDB).CountPairings(ctx, channelExternalID)
}

func (d *DB) InsertChannelPairingRecord(ctx context.Context, channelExternalID string, pairing ChannelPairingInput) error {
	return NewChannelRecordMapper(d.mapperDB).InsertPairing(ctx, insertChannelPairingParams{
		ExternalID:        pairing.ExternalID,
		ChannelExternalID: channelExternalID,
		Kind:              pairing.Kind,
		Target:            pairing.Target,
		DisplayName:       pairing.DisplayName,
	})
}

func (d *DB) UpdateChannelPairingStatus(ctx context.Context, channelExternalID, pairingExternalID, status string) error {
	return NewChannelRecordMapper(d.mapperDB).UpdatePairingStatus(ctx, channelExternalID, pairingExternalID, status)
}

func (d *DB) GetChannelSessionBinding(ctx context.Context, channelExternalID, scopeKey string) (string, error) {
	sessionExternalID, err := NewChannelRecordMapper(d.mapperDB).FindChannelSession(ctx, channelExternalID, scopeKey)
	return sessionExternalID, mapNoRows(err)
}

func (d *DB) SetChannelSessionBinding(ctx context.Context, channelExternalID, scopeKey, sessionExternalID string) error {
	return NewChannelRecordMapper(d.mapperDB).UpsertChannelSession(ctx, channelExternalID, scopeKey, sessionExternalID)
}

func (d *DB) DeleteChannelSessionBinding(ctx context.Context, channelExternalID, scopeKey string) error {
	return NewChannelRecordMapper(d.mapperDB).DeleteChannelSession(ctx, channelExternalID, scopeKey)
}

func (d *DB) ClearChannelSessionBindings(ctx context.Context, channelExternalID string) error {
	return NewChannelRecordMapper(d.mapperDB).ArchiveChannelSessions(ctx, channelExternalID)
}

func (d *DB) GetChannelAgentEnvironment(ctx context.Context, organizationUUID, workspaceUUID, agentExternalID string) (string, error) {
	environmentExternalID, err := NewChannelRecordMapper(d.mapperDB).FindChannelAgentEnvironment(ctx, organizationUUID, workspaceUUID, agentExternalID)
	return environmentExternalID, mapNoRows(err)
}

func (d *DB) SetChannelAgentEnvironment(ctx context.Context, organizationUUID, workspaceUUID, agentExternalID, environmentExternalID string) error {
	return NewChannelRecordMapper(d.mapperDB).UpsertChannelAgentEnvironment(ctx, organizationUUID, workspaceUUID, agentExternalID, environmentExternalID)
}

type ChannelRecordInput struct {
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

type ChannelRecordUpdate struct {
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

type ChannelPairingInput struct {
	ExternalID  string
	Kind        string
	Target      string
	DisplayName string
}

package channels

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

func personalVaultIdentity(record db.ChannelRecord, actorID string) db.ChannelUserVaultIdentity {
	return db.ChannelUserVaultIdentity{OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID, ChannelType: record.ChannelType, AppID: record.AppID, ActorID: actorID}
}

func (s *Service) ensurePersonalVault(ctx context.Context, record db.ChannelRecord, actorID string) (string, error) {
	identity := personalVaultIdentity(record, actorID)
	id, err := s.store.GetChannelUserVaultID(ctx, identity)
	if errors.Is(err, db.ErrNotFound) {
		id, err = ids.New("vlt_")
		if err != nil {
			return "", err
		}
		metadata, encodeErr := jsonv2.Marshal(map[string]string{"source": "channel-oauth", "channel_type": record.ChannelType, "app_id": record.AppID, "actor_id": actorID})
		if encodeErr != nil {
			return "", encodeErr
		}
		id, err = s.store.EnsureChannelUserVault(ctx, identity, db.Vault{
			UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID,
			DisplayName: "渠道个人连接 · " + actorID, Metadata: json.RawMessage(metadata), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}
	if err != nil {
		return "", err
	}
	current, err := s.store.GetVault(ctx, record.WorkspaceUUID, id)
	if err != nil {
		return "", err
	}
	if current.ArchivedAt != nil || current.DeletedAt != nil {
		return "", errors.New("个人连接保险库已停用")
	}
	return current.ExternalID, nil
}

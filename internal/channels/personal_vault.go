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

func (s *Service) personalVaultIdentity(ctx context.Context, record db.ChannelRecord, actorID string, create bool) (db.ChannelUserVaultIdentity, error) {
	key := db.ChannelUserVaultIdentity{OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID, ChannelType: record.ChannelType, AppID: record.AppID, ActorID: actorID}
	if s.supervisor == nil || s.supervisor.ingress == nil {
		return key, nil
	}
	ingress := channelIngress(record, &channelMessage{UserID: actorID, ChatType: "p2p"}, "")
	owner, err := s.supervisor.ingress.PersonalActorIdentity(ctx, ingress, create)
	if !create && errors.Is(err, db.ErrNotFound) {
		return key, nil
	}
	if err != nil {
		return key, err
	}
	key.IdentityUUID = owner.UUID
	if !owner.Enabled {
		return key, errChannelPersonalIdentityDisabled
	}
	if create {
		return key, s.store.AdoptChannelUserVault(ctx, key)
	}
	return key, nil
}

func (s *Service) lookupPersonalVault(ctx context.Context, record db.ChannelRecord, actorID string) (string, bool, error) {
	identity, err := s.personalVaultIdentity(ctx, record, actorID, false)
	if err != nil {
		return "", false, err
	}
	id, err := s.store.GetChannelUserVaultID(ctx, identity)
	if errors.Is(err, db.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	current, err := s.store.GetVault(ctx, record.WorkspaceUUID, id)
	if errors.Is(err, db.ErrNotFound) || current.ArchivedAt != nil || current.DeletedAt != nil {
		return id, false, nil
	}
	if err != nil {
		return "", false, err
	}
	return current.ExternalID, true, nil
}

func (s *Service) ensurePersonalVault(ctx context.Context, record db.ChannelRecord, actorID string) (string, error) {
	identity, err := s.personalVaultIdentity(ctx, record, actorID, true)
	if err != nil {
		return "", err
	}
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
		id, err = ids.New("vlt_")
		if err != nil {
			return "", err
		}
		metadata, encodeErr := jsonv2.Marshal(map[string]string{"source": "channel-oauth", "channel_type": record.ChannelType, "app_id": record.AppID, "actor_id": actorID})
		if encodeErr != nil {
			return "", encodeErr
		}
		return s.store.ReplaceChannelUserVault(ctx, identity, db.Vault{
			UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID,
			DisplayName: "渠道个人连接 · " + actorID, Metadata: json.RawMessage(metadata), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}
	return current.ExternalID, nil
}

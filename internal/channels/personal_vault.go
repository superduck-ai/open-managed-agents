package channels

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func personalVaultID(record db.ChannelRecord, actorID string) string {
	return fmt.Sprintf("vlt_channel_%x", sha256.Sum256([]byte(record.OrganizationUUID+"\x00"+record.WorkspaceUUID+"\x00"+record.ChannelType+"\x00"+record.AppID+"\x00"+actorID)))
}

func (s *Service) ensurePersonalVault(ctx context.Context, record db.ChannelRecord, actorID string) (string, error) {
	id := personalVaultID(record, actorID)
	current, err := s.store.GetVault(ctx, record.WorkspaceUUID, id)
	if errors.Is(err, db.ErrNotFound) {
		metadata, encodeErr := jsonv2.Marshal(map[string]string{"source": "channel-oauth", "channel_type": record.ChannelType, "app_id": record.AppID, "actor_id": actorID})
		if encodeErr != nil {
			return "", encodeErr
		}
		current, err = s.store.CreateVault(ctx, db.Vault{
			UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID,
			DisplayName: "渠道个人连接 · " + actorID, Metadata: json.RawMessage(metadata), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			current, err = s.store.GetVault(ctx, record.WorkspaceUUID, id)
		}
	}
	if err != nil {
		return "", err
	}
	if current.ArchivedAt != nil || current.DeletedAt != nil {
		return "", errors.New("个人连接保险库已停用")
	}
	return current.ExternalID, nil
}

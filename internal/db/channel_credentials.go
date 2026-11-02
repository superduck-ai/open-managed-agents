package db

import (
	"context"
	"encoding/json/v2"
	"errors"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/yourbatis"
)

var ErrChannelCredentialInactive = errors.New("渠道专用 API Key 已停用，请在 API 密钥管理中恢复")

func (d *DB) EnsureChannelWorkspaceCredential(ctx context.Context, input platform.CreateConsoleAPIKeyInput, cipher *secrets.Service) (string, error) {
	var rawKey string
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewChannelCredentialMapper(executor)
		displayID, err := mapper.LockWorkspace(ctx, input.OrgUUID, input.WorkspaceUUID)
		if err != nil {
			return err
		}
		row, found, err := mapper.Find(ctx, input.OrgUUID, input.WorkspaceUUID)
		if err != nil {
			return err
		}
		binding := secrets.Binding{OrganizationUUID: input.OrgUUID, WorkspaceUUID: input.WorkspaceUUID, VaultExternalID: "channel-runtime", CredentialExternalID: row.ExternalID}
		if found {
			if row.Status != "active" {
				return ErrChannelCredentialInactive
			}
			var envelope secrets.Envelope
			if err := json.Unmarshal([]byte(row.Envelope), &envelope); err != nil {
				return err
			}
			plaintext, err := cipher.Open(ctx, binding, envelope)
			defer clear(plaintext)
			if err != nil {
				return err
			}
			rawKey = string(plaintext)
			return nil
		}
		externalID, keyUUID := consolePrefixedID("apikey", 18), uuid.NewV4().String()
		binding.CredentialExternalID = externalID
		rawKey = "sk-ant-api03-" + consoleRandomToken(32)
		envelope, err := cipher.Seal(ctx, binding, []byte(rawKey))
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		input.Name, input.WorkspaceDisplayID = "飞书渠道专用", displayID
		if _, err := insertConsoleAPIKey(ctx, executor, input, externalID, keyUUID, rawKey); err != nil {
			return err
		}
		return mapper.Insert(ctx, insertChannelCredentialParams{ExternalID: externalID, OrganizationUUID: input.OrgUUID, WorkspaceUUID: input.WorkspaceUUID, APIKeyUUID: keyUUID, Envelope: string(encoded)})
	})
	if err != nil {
		return "", err
	}
	return rawKey, nil
}

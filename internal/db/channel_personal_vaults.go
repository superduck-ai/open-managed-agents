package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

type ChannelPersonalVaultIdentity struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ChannelType      string
	AppID            string
	ActorID          string
}

func (d *DB) GetChannelPersonalVaultID(ctx context.Context, identity ChannelPersonalVaultIdentity) (string, error) {
	id, found, err := NewChannelPersonalVaultMapper(d.mapperDB).Find(ctx, identity)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrNotFound
	}
	return id, nil
}

func (d *DB) EnsureChannelPersonalVault(ctx context.Context, identity ChannelPersonalVaultIdentity, vault Vault) (string, error) {
	var id string
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if _, err := NewChannelCredentialMapper(executor).LockWorkspace(ctx, identity.OrganizationUUID, identity.WorkspaceUUID); err != nil {
			return mapNoRows(err)
		}
		mapper := NewChannelPersonalVaultMapper(executor)
		current, found, err := mapper.Find(ctx, identity)
		if err != nil {
			return err
		}
		if found {
			id = current
			return nil
		}
		created, err := NewVaultMapper(executor).Insert(ctx, vaultInsertParams(vault))
		if err != nil {
			return err
		}
		if err := mapper.Insert(ctx, identity, created.UUID); err != nil {
			return err
		}
		id = created.ExternalID
		return nil
	})
	return id, err
}

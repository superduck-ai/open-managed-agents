package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

type ChannelUserVaultIdentity struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ChannelType      string
	AppID            string
	ActorID          string
	IdentityUUID     string
}

func (d *DB) GetChannelUserVaultID(ctx context.Context, identity ChannelUserVaultIdentity) (string, error) {
	id, found, err := NewChannelUserVaultMapper(d.mapperDB).Find(ctx, identity)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrNotFound
	}
	return id, nil
}

func (d *DB) AdoptChannelUserVault(ctx context.Context, identity ChannelUserVaultIdentity) error {
	return NewChannelUserVaultMapper(d.mapperDB).Adopt(ctx, identity)
}

func (d *DB) ReplaceChannelUserVault(ctx context.Context, identity ChannelUserVaultIdentity, vault Vault) (string, error) {
	var id string
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if _, err := NewChannelCredentialMapper(executor).LockWorkspace(ctx, identity.OrganizationUUID, identity.WorkspaceUUID); err != nil {
			return mapNoRows(err)
		}
		created, err := NewVaultMapper(executor).Insert(ctx, vaultInsertParams(vault))
		if err != nil {
			return err
		}
		if err := saveChannelUserVault(ctx, NewChannelUserVaultMapper(executor), identity, created.UUID); err != nil {
			return err
		}
		id = created.ExternalID
		return nil
	})
	return id, err
}

func (d *DB) EnsureChannelUserVault(ctx context.Context, identity ChannelUserVaultIdentity, vault Vault) (string, error) {
	var id string
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if _, err := NewChannelCredentialMapper(executor).LockWorkspace(ctx, identity.OrganizationUUID, identity.WorkspaceUUID); err != nil {
			return mapNoRows(err)
		}
		mapper := NewChannelUserVaultMapper(executor)
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
		if err := saveChannelUserVault(ctx, mapper, identity, created.UUID); err != nil {
			return err
		}
		id = created.ExternalID
		return nil
	})
	return id, err
}

func saveChannelUserVault(ctx context.Context, mapper ChannelUserVaultMapper, identity ChannelUserVaultIdentity, vaultUUID string) error {
	count, err := mapper.Insert(ctx, identity, vaultUUID)
	if err == nil && count == 0 {
		return ErrInvalidState
	}
	return err
}

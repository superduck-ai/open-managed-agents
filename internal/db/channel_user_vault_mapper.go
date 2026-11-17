package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper ChannelUserVaultMapper -sql ./channel_user_vault_mapper.xml -out ./channel_user_vault_mapper.sqlmap.gen.go -dialect postgres

type ChannelUserVaultMapper interface {
	Find(ctx context.Context, identity ChannelUserVaultIdentity) (string, bool, error)
	Insert(ctx context.Context, identity ChannelUserVaultIdentity, vaultUUID string) error
}

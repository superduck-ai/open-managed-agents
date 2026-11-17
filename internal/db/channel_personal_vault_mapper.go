package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper ChannelPersonalVaultMapper -sql ./channel_personal_vault_mapper.xml -out ./channel_personal_vault_mapper.sqlmap.gen.go -dialect postgres

type ChannelPersonalVaultMapper interface {
	Find(ctx context.Context, identity ChannelPersonalVaultIdentity) (string, bool, error)
	Insert(ctx context.Context, identity ChannelPersonalVaultIdentity, vaultUUID string) error
}

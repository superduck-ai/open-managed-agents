package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper ChannelInputMapper -sql ./channel_input_mapper.xml -out ./channel_input_mapper.sqlmap.gen.go -dialect postgres

type ChannelInputMapper interface {
	Insert(ctx context.Context, scope ChannelInputScope, input ChannelInput) (ChannelInput, bool, error)
	Claim(ctx context.Context, scope ChannelInputScope, inputUUID, owner string) (ChannelInput, bool, error)
	ListRecoverable(ctx context.Context, scope ChannelInputScope) ([]ChannelInput, error)
	HoldOAuth(ctx context.Context, scope ChannelInputScope, inputUUID, owner, patch string) (int64, error)
	PairingInput(ctx context.Context, scope ChannelInputScope, inputUUID string) (ChannelInput, bool, error)
	HasPairingWait(ctx context.Context, scope ChannelInputScope, input ChannelPairingResume) (bool, error)
	ResumeAfterPairing(ctx context.Context, scope ChannelInputScope, input ChannelPairingResume) (string, bool, error)
	ResumeOAuthWaiters(ctx context.Context, scope ChannelInputScope, input ChannelPairingResume, provider string) ([]ChannelInput, error)
	LockSources(ctx context.Context, scope ChannelInputScope, owner string, batch ChannelInputBatch) ([]ChannelInput, error)
	GroupSources(ctx context.Context, scope ChannelInputScope, owner, anchorUUID string, batch ChannelInputBatch) error
	Prepare(ctx context.Context, scope ChannelInputScope, inputUUID, owner string, batch ChannelInputBatch) (ChannelInput, bool, error)
	Advance(ctx context.Context, scope ChannelInputScope, inputUUID, owner string, change ChannelInputAdvance) (ChannelInput, bool, error)
	Complete(ctx context.Context, scope ChannelInputScope, inputUUID, owner string) (string, bool, error)
	Release(ctx context.Context, scope ChannelInputScope, inputUUID, owner string) error
	Renew(ctx context.Context, scope ChannelInputScope, owner string) error
}

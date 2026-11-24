package db

import (
	"context"
	"encoding/json"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper HarnessStateMapper -sql ./harness_state_mapper.xml -out ./harness_state_mapper.sqlmap.gen.go -dialect postgres

type harnessStateRow struct {
	MessageCount        int64           `db:"message_count"`
	Version             uint64          `db:"version"`
	Metadata            *string         `db:"metadata"`
	ActiveSubscriptions json.RawMessage `db:"active_subscriptions"`
	PendingUserChoices  json.RawMessage `db:"pending_user_choices"`
}

type HarnessStateMapper interface {
	FindState(ctx context.Context, key harnessThreadKey) (harnessStateRow, bool, error)
	SaveState(ctx context.Context, key harnessThreadKey, state harnessStateRow) error
	DeleteStates(ctx context.Context, scope harnessConversationScope) error
}

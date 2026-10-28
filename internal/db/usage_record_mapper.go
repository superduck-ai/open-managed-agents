package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper UsageRecordMapper -sql ./usage_record_mapper.xml -out ./usage_record_mapper.sqlmap.gen.go -dialect postgres

type usageAggregateRow struct {
	Day                string `db:"day"`
	Model              string `db:"model"`
	WorkspaceUUID      string `db:"workspace_uuid"`
	Source             string `db:"source"`
	APIKeyUUID         string `db:"api_key_uuid"`
	Requests           int64  `db:"requests"`
	InputTokens        int64  `db:"input_tokens"`
	OutputTokens       int64  `db:"output_tokens"`
	CacheReadTokens    int64  `db:"cache_read_tokens"`
	CacheWriteTokens   int64  `db:"cache_write_tokens"`
	CacheWrite5mTokens int64  `db:"cache_write_5m_tokens"`
	CacheWrite1hTokens int64  `db:"cache_write_1h_tokens"`
	SearchRequests     int64  `db:"search_requests"`
	ModelCents         int64  `db:"model_cents"`
	SearchCents        int64  `db:"search_cents"`
	Unpriced           int64  `db:"unpriced"`
	Incomplete         int64  `db:"incomplete"`
}

type modelUsageRow struct {
	Day                string `db:"day"`
	Model              string `db:"model"`
	PriceSnapshot      []byte `db:"price_snapshot"`
	InputTokens        int64  `db:"input_tokens"`
	OutputTokens       int64  `db:"output_tokens"`
	CacheReadTokens    int64  `db:"cache_read_tokens"`
	CacheWriteTokens   int64  `db:"cache_write_tokens"`
	CacheWrite5mTokens int64  `db:"cache_write_5m_tokens"`
	CacheWrite1hTokens int64  `db:"cache_write_1h_tokens"`
	ModelCents         int64  `db:"model_cents"`
	ModelCostUSD       string `db:"model_cost_usd"`
}

type usageDimensionRow struct {
	Model      string    `db:"model"`
	APIKeyUUID string    `db:"api_key_uuid"`
	FirstSeen  time.Time `db:"first_seen"`
}

type UsageRecordMapper interface {
	Dimensions(ctx context.Context, query UsageQuery) ([]usageDimensionRow, error)
	Insert(ctx context.Context, record UsageRecord) error
	Aggregate(ctx context.Context, query UsageQuery) ([]usageAggregateRow, error)
	ListModelUsage(ctx context.Context, query UsageQuery) ([]modelUsageRow, error)
	ListUnmeteredSessionEvents(ctx context.Context, limit int) ([]sessionEventRow, error)
}

package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type UsageRecord struct {
	PriceSnapshot      json.RawMessage
	Finalized          bool
	OrganizationUUID   string
	WorkspaceUUID      string
	AttemptID          string
	Source             string
	SessionUUID        *string
	SessionExternalID  string
	APIKeyUUID         *string
	UserUUID           *string
	Model              string
	StartedAt          time.Time
	EndedAt            time.Time
	InputTokens        int64
	OutputTokens       int64
	CacheReadTokens    int64
	CacheWriteTokens   int64
	CacheWrite5mTokens int64
	CacheWrite1hTokens int64
	SearchRequests     int64
	ModelCents         *int64
	ModelCostUSD       string
	SearchCents        int64
	Complete           bool
	Version            string
}

type UsageQuery struct {
	OrganizationUUID string
	WorkspaceUUIDs   []string
	Start            time.Time
	End              time.Time
	AsOf             time.Time
	Timezone         string
	Model            string
	Source           string
	SessionID        string
	APIKeyUUID       string
}

type UsageAggregate struct {
	Day                string
	Model              string
	WorkspaceUUID      string
	Source             string
	APIKeyUUID         string
	Requests           int64
	InputTokens        int64
	OutputTokens       int64
	CacheReadTokens    int64
	CacheWriteTokens   int64
	CacheWrite5mTokens int64
	CacheWrite1hTokens int64
	SearchRequests     int64
	ModelCents         int64
	SearchCents        int64
	Unpriced           int64
	Incomplete         int64
}

func (d *DB) InsertUsageRecord(ctx context.Context, record UsageRecord) error {
	return NewUsageRecordMapper(d.mapperDB).Insert(ctx, record)
}

type ModelUsage struct {
	Day                string
	Model              string
	PriceSnapshot      []byte
	InputTokens        int64
	OutputTokens       int64
	CacheReadTokens    int64
	CacheWriteTokens   int64
	CacheWrite5mTokens int64
	CacheWrite1hTokens int64
	ModelCents         int64
	ModelCostUSD       string
}

func (d *DB) ListModelUsage(ctx context.Context, query UsageQuery) ([]ModelUsage, error) {
	if len(query.WorkspaceUUIDs) == 0 {
		return []ModelUsage{}, nil
	}
	rows, err := NewUsageRecordMapper(d.mapperDB).ListModelUsage(ctx, query)
	if err != nil {
		return nil, err
	}
	usage := make([]ModelUsage, len(rows))
	for i, row := range rows {
		usage[i] = ModelUsage{
			Day: row.Day, Model: row.Model, PriceSnapshot: row.PriceSnapshot,
			InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheReadTokens: row.CacheReadTokens, CacheWriteTokens: row.CacheWriteTokens,
			CacheWrite5mTokens: row.CacheWrite5mTokens, CacheWrite1hTokens: row.CacheWrite1hTokens,
			ModelCents: row.ModelCents, ModelCostUSD: row.ModelCostUSD,
		}
	}
	return usage, nil
}

func (d *DB) AggregateUsage(ctx context.Context, query UsageQuery) ([]UsageAggregate, error) {
	if len(query.WorkspaceUUIDs) == 0 {
		return []UsageAggregate{}, nil
	}
	rows, err := NewUsageRecordMapper(d.mapperDB).Aggregate(ctx, query)
	if err != nil {
		return nil, err
	}
	result := make([]UsageAggregate, len(rows))
	for i, row := range rows {
		result[i] = UsageAggregate(row)
	}
	return result, nil
}

func (d *DB) BackfillUsageRecords(ctx context.Context, limit int) (int, error) {
	rows, err := NewUsageRecordMapper(d.mapperDB).ListUnmeteredSessionEvents(ctx, limit)
	if err != nil {
		return 0, err
	}
	err = d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		for _, row := range rows {
			if err := insertSessionUsageRecord(ctx, executor, row.event()); err != nil {
				return err
			}
		}
		return nil
	})
	return len(rows), err
}

type UsageDimension struct {
	Model      string
	APIKeyUUID string
	FirstSeen  time.Time
}

func (d *DB) UsageDimensions(ctx context.Context, query UsageQuery) ([]UsageDimension, error) {
	if len(query.WorkspaceUUIDs) == 0 {
		return []UsageDimension{}, nil
	}
	rows, err := NewUsageRecordMapper(d.mapperDB).Dimensions(ctx, query)
	if err != nil {
		return nil, err
	}
	result := make([]UsageDimension, len(rows))
	for i, row := range rows {
		result[i] = UsageDimension(row)
	}
	return result, nil
}

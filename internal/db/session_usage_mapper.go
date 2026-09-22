package db

import (
	"context"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SessionUsageMapper -sql ./session_usage_mapper.xml -out ./session_usage_mapper.sqlmap.gen.go -dialect postgres

type sessionUsageTotalsRow struct {
	ListCostCents            int64 `db:"list_cost_cents"`
	InputTokens              int64 `db:"input_tokens"`
	OutputTokens             int64 `db:"output_tokens"`
	CacheReadInputTokens     int64 `db:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `db:"cache_creation_input_tokens"`
	WebSearchRequests        int64 `db:"web_search_requests"`
}

// SessionUsageTotals is the exported aggregate of metered session usage.
type SessionUsageTotals struct {
	ListCostCents            int64
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
	WebSearchRequests        int64
	ActiveSeconds            float64
}

type sessionUsageTotalsParams struct {
	WorkspaceUUID    string
	SessionExternalID string
	ThreadExternalID string
}

// SessionUsageMapper aggregates billing and token usage from persisted
// span.model_request_end events. The billing payload is written by the
// platform when the event is synthesized, so the sum is the authoritative
// session list cost the budget is enforced against.
type SessionUsageMapper interface {
	SumUsageTotals(ctx context.Context, params sessionUsageTotalsParams) (sessionUsageTotalsRow, bool, error)
	SumActiveSeconds(ctx context.Context, params sessionUsageTotalsParams) (float64, error)
}

type sessionUsageScope struct {
	workspaceUUID     string
	sessionExternalID string
}

func usageTotalsParams(workspaceUUID, sessionExternalID, threadExternalID string) sessionUsageTotalsParams {
	return sessionUsageTotalsParams{
		WorkspaceUUID:    workspaceUUID,
		SessionExternalID: sessionExternalID,
		ThreadExternalID: threadExternalID,
	}
}

func (d *DB) SumSessionUsageTotals(ctx context.Context, workspaceUUID, sessionExternalID string) (SessionUsageTotals, error) {
	return d.sumUsageTotals(ctx, usageTotalsParams(workspaceUUID, sessionExternalID, ""))
}

func (d *DB) SumSessionThreadUsageTotals(ctx context.Context, workspaceUUID, sessionExternalID, threadExternalID string) (SessionUsageTotals, error) {
	return d.sumUsageTotals(ctx, usageTotalsParams(workspaceUUID, sessionExternalID, threadExternalID))
}

func (d *DB) sumUsageTotals(ctx context.Context, params sessionUsageTotalsParams) (SessionUsageTotals, error) {
	row, _, err := NewSessionUsageMapper(d.mapperDB).SumUsageTotals(ctx, params)
	if err != nil {
		return SessionUsageTotals{}, err
	}
	activeSeconds, err := NewSessionUsageMapper(d.mapperDB).SumActiveSeconds(ctx, params)
	if err != nil {
		return SessionUsageTotals{}, err
	}
	return SessionUsageTotals{
		ListCostCents:            row.ListCostCents,
		InputTokens:              row.InputTokens,
		OutputTokens:             row.OutputTokens,
		CacheReadInputTokens:     row.CacheReadInputTokens,
		CacheCreationInputTokens: row.CacheCreationInputTokens,
		WebSearchRequests:        row.WebSearchRequests,
		ActiveSeconds:            activeSeconds,
	}, nil
}

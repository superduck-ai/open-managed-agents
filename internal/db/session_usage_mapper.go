package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper SessionUsageMapper -sql ./session_usage_mapper.xml -out ./session_usage_mapper.sqlmap.gen.go -dialect postgres

type sessionUsageTotalsRow struct {
	ListCostCents            int64 `db:"list_cost_cents"`
	InputTokens              int64 `db:"input_tokens"`
	OutputTokens             int64 `db:"output_tokens"`
	CacheReadInputTokens     int64 `db:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `db:"cache_creation_input_tokens"`
	CacheWrite5mTokens       int64 `db:"cache_write_5m_tokens"`
	CacheWrite1hTokens       int64 `db:"cache_write_1h_tokens"`
	WebSearchRequests        int64 `db:"web_search_requests"`
}

type sessionUsageTotalsParams struct {
	WorkspaceUUID     string
	SessionExternalID string
	ThreadExternalID  string
}

type SessionUsageMapper interface {
	SumUsageTotals(ctx context.Context, params sessionUsageTotalsParams) (sessionUsageTotalsRow, bool, error)
	SumActiveSeconds(ctx context.Context, params sessionUsageTotalsParams) (float64, error)
}

func usageTotalsParams(workspaceUUID, sessionExternalID, threadExternalID string) sessionUsageTotalsParams {
	return sessionUsageTotalsParams{
		WorkspaceUUID:     workspaceUUID,
		SessionExternalID: sessionExternalID,
		ThreadExternalID:  threadExternalID,
	}
}

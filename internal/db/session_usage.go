package db

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/yourbatis"
)

type SessionUsageTotals struct {
	ListCostCents            int64
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
	CacheWrite5mTokens       int64
	CacheWrite1hTokens       int64
	WebSearchRequests        int64
	ActiveSeconds            float64
}

func (d *DB) SumSessionUsageTotals(ctx context.Context, workspaceUUID, sessionExternalID string) (SessionUsageTotals, error) {
	return sumUsageTotals(ctx, NewSessionUsageMapper(d.mapperDB), usageTotalsParams(workspaceUUID, sessionExternalID, ""))
}

func (d *DB) SumSessionThreadUsageTotals(ctx context.Context, workspaceUUID, sessionExternalID, threadExternalID string) (SessionUsageTotals, error) {
	return sumUsageTotals(ctx, NewSessionUsageMapper(d.mapperDB), usageTotalsParams(workspaceUUID, sessionExternalID, threadExternalID))
}

func sumUsageTotals(ctx context.Context, mapper SessionUsageMapper, params sessionUsageTotalsParams) (SessionUsageTotals, error) {
	row, _, err := mapper.SumUsageTotals(ctx, params)
	if err != nil {
		return SessionUsageTotals{}, err
	}
	activeSeconds, err := mapper.SumActiveSeconds(ctx, params)
	if err != nil {
		return SessionUsageTotals{}, err
	}
	return SessionUsageTotals{
		ListCostCents:            row.ListCostCents,
		InputTokens:              row.InputTokens,
		OutputTokens:             row.OutputTokens,
		CacheReadInputTokens:     row.CacheReadInputTokens,
		CacheCreationInputTokens: row.CacheCreationInputTokens,
		CacheWrite5mTokens:       row.CacheWrite5mTokens,
		CacheWrite1hTokens:       row.CacheWrite1hTokens,
		WebSearchRequests:        row.WebSearchRequests,
		ActiveSeconds:            activeSeconds,
	}, nil
}

func (d *DB) CheckSessionBudget(ctx context.Context, session Session, limit int64) error {
	return checkSessionBudget(ctx, d.mapperDB, session, limit)
}

func checkSessionBudget(ctx context.Context, executor yourbatis.Executor, session Session, limit int64) error {
	if limit == 0 {
		return nil
	}
	totals, err := sumUsageTotals(ctx, NewSessionUsageMapper(executor), usageTotalsParams(session.WorkspaceUUID, session.ExternalID, ""))
	if err != nil {
		return err
	}
	if limit <= billing.TotalListCostCents(totals.ListCostCents, totals.WebSearchRequests, totals.ActiveSeconds) {
		return ErrSessionBudgetNotRaised
	}
	return nil
}

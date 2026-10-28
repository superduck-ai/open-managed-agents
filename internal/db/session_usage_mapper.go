package db

import (
	"context"
	"encoding/json"
	"math/big"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/yourbatis"
)

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

type webSearchDayRow struct {
	Day      string `db:"day"`
	Name     string `db:"name"`
	CNY      string `db:"cny"`
	Per      string `db:"per"`
	Exchange string `db:"exchange"`
	Searches int64  `db:"searches"`
}

type toolPriceCountRow struct {
	CNY      string `db:"cny"`
	Per      string `db:"per"`
	Exchange string `db:"exchange"`
	Searches int64  `db:"searches"`
}

type sessionModelPayloadRow struct {
	Payload []byte `db:"payload"`
}

type WebSearchDay struct {
	Day      string
	Name     string
	CNY      string
	Per      string
	Exchange string
	Searches int64
}

// SessionUsageTotals is the exported aggregate of metered session usage.
type SessionUsageTotals struct {
	ListCostCents             int64
	InputTokens               int64
	OutputTokens              int64
	CacheReadInputTokens      int64
	CacheCreationInputTokens  int64
	CacheWrite5mTokens        int64
	CacheWrite1hTokens        int64
	WebSearchRequests         int64
	ListCostUSD               string
	ModelCostUSD              string
	ToolCostUSD               string
	ModelRequestMilliseconds  int64
	SessionActiveMilliseconds int64
	RuntimeCostUSD            string
}

type sessionUsageTotalsParams struct {
	WorkspaceUUID     string
	SessionExternalID string
	ThreadExternalID  string
}

// SessionUsageMapper aggregates billing and token usage from persisted
// span.model_request_end events. Successful web_search calls are counted from
// agent.tool_result events. Tool cents use the price recorded on each result.
type SessionUsageMapper interface {
	SumUsageTotals(ctx context.Context, params sessionUsageTotalsParams) (sessionUsageTotalsRow, bool, error)
	SumModelRequestMilliseconds(ctx context.Context, params sessionUsageTotalsParams) (int64, error)
	CountWebSearches(ctx context.Context, query UsageQuery) ([]webSearchDayRow, error)
	ListSessionToolPrices(ctx context.Context, params sessionUsageTotalsParams) ([]toolPriceCountRow, error)
	ListSessionModelPayloads(ctx context.Context, params sessionUsageTotalsParams) ([]sessionModelPayloadRow, error)
}

func usageTotalsParams(workspaceUUID, sessionExternalID, threadExternalID string) sessionUsageTotalsParams {
	return sessionUsageTotalsParams{
		WorkspaceUUID:     workspaceUUID,
		SessionExternalID: sessionExternalID,
		ThreadExternalID:  threadExternalID,
	}
}

func (d *DB) SumSessionUsageTotals(ctx context.Context, workspaceUUID, sessionExternalID string) (SessionUsageTotals, error) {
	return d.sumUsageTotals(ctx, usageTotalsParams(workspaceUUID, sessionExternalID, ""))
}

func (d *DB) SumSessionThreadUsageTotals(ctx context.Context, workspaceUUID, sessionExternalID, threadExternalID string) (SessionUsageTotals, error) {
	return d.sumUsageTotals(ctx, usageTotalsParams(workspaceUUID, sessionExternalID, threadExternalID))
}

func (d *DB) CountWebSearches(ctx context.Context, query UsageQuery) ([]WebSearchDay, error) {
	if len(query.WorkspaceUUIDs) == 0 {
		return []WebSearchDay{}, nil
	}
	rows, err := NewSessionUsageMapper(d.mapperDB).CountWebSearches(ctx, query)
	if err != nil {
		return nil, err
	}
	counts := make([]WebSearchDay, len(rows))
	for i, row := range rows {
		counts[i] = WebSearchDay{Day: row.Day, Name: row.Name, CNY: row.CNY, Per: row.Per, Exchange: row.Exchange, Searches: row.Searches}
	}
	return counts, nil
}

func (d *DB) sumUsageTotals(ctx context.Context, params sessionUsageTotalsParams) (SessionUsageTotals, error) {
	mapper := NewSessionUsageMapper(d.mapperDB)
	row, _, err := mapper.SumUsageTotals(ctx, params)
	if err != nil {
		return SessionUsageTotals{}, err
	}
	prices, err := mapper.ListSessionToolPrices(ctx, params)
	if err != nil {
		return SessionUsageTotals{}, err
	}
	payloads, err := mapper.ListSessionModelPayloads(ctx, params)
	if err != nil {
		return SessionUsageTotals{}, err
	}
	modelRequestMilliseconds, err := mapper.SumModelRequestMilliseconds(ctx, params)
	if err != nil {
		return SessionUsageTotals{}, err
	}
	totals := SessionUsageTotals{
		ListCostCents:            row.ListCostCents,
		InputTokens:              row.InputTokens,
		OutputTokens:             row.OutputTokens,
		CacheReadInputTokens:     row.CacheReadInputTokens,
		CacheCreationInputTokens: row.CacheCreationInputTokens,
		CacheWrite5mTokens:       row.CacheWrite5mTokens,
		CacheWrite1hTokens:       row.CacheWrite1hTokens,
		WebSearchRequests:        row.WebSearchRequests,
		ModelRequestMilliseconds: modelRequestMilliseconds,
	}
	model, tool := sessionUsageParts(payloads, prices)
	totals.ModelCostUSD = billing.FormatDollars(model)
	totals.ToolCostUSD = billing.FormatDollars(tool)
	dollars := new(big.Rat).Add(model, tool)
	if params.ThreadExternalID != "" {
		totals.ListCostUSD = billing.FormatDollars(dollars)
		return totals, nil
	}
	session, found, err := NewSessionMapper(d.mapperDB).FindByExternalID(ctx, params.WorkspaceUUID, params.SessionExternalID)
	if err != nil || !found {
		totals.ListCostUSD = billing.FormatDollars(dollars)
		return totals, err
	}
	totals.SessionActiveMilliseconds, totals.RuntimeCostUSD = usageDurations(session.session().Usage)
	if runtime, ok := new(big.Rat).SetString(totals.RuntimeCostUSD); ok && runtime.Sign() > 0 {
		dollars.Add(dollars, runtime)
	}
	totals.ListCostUSD = billing.FormatDollars(dollars)
	return totals, nil
}

func sessionUsageParts(payloads []sessionModelPayloadRow, prices []toolPriceCountRow) (*big.Rat, *big.Rat) {
	model := new(big.Rat)
	tool := new(big.Rat)
	for _, row := range payloads {
		model.Add(model, modelRequestDollars(row.Payload))
	}
	for _, row := range prices {
		tool.Add(tool, billing.ResolveSearchPrice(row.CNY, row.Per, row.Exchange).Dollars(row.Searches))
	}
	return model, tool
}

func usageListCostWithRuntime(ctx context.Context, executor yourbatis.Executor, workspaceUUID, sessionExternalID string, usage json.RawMessage) (json.RawMessage, error) {
	mapper := NewSessionUsageMapper(executor)
	params := usageTotalsParams(workspaceUUID, sessionExternalID, "")
	payloads, err := mapper.ListSessionModelPayloads(ctx, params)
	if err != nil {
		return nil, err
	}
	prices, err := mapper.ListSessionToolPrices(ctx, params)
	if err != nil {
		return nil, err
	}
	model, tool := sessionUsageParts(payloads, prices)
	dollars := new(big.Rat).Add(model, tool)
	_, runtime := usageDurations(usage)
	if amount, ok := new(big.Rat).SetString(runtime); ok && amount.Sign() > 0 {
		dollars.Add(dollars, amount)
	}
	return setUsageCosts(usage, billing.FormatDollars(dollars), billing.FormatDollars(model), billing.FormatDollars(tool))
}

func setUsageCosts(usage json.RawMessage, listCost, modelCost, toolCost string) (json.RawMessage, error) {
	payload := map[string]json.RawMessage{}
	if len(usage) > 0 {
		if err := json.Unmarshal(usage, &payload); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(map[string]string{"amount": listCost, "currency": "USD"})
	if err != nil {
		return nil, err
	}
	modelRaw, err := json.Marshal(modelCost)
	if err != nil {
		return nil, err
	}
	toolRaw, err := json.Marshal(toolCost)
	if err != nil {
		return nil, err
	}
	payload["list_cost"] = raw
	payload["model_cost_usd"] = modelRaw
	payload["tool_cost_usd"] = toolRaw
	return json.Marshal(payload)
}

func usageDurations(usage json.RawMessage) (int64, string) {
	if len(usage) == 0 {
		return 0, ""
	}
	var payload struct {
		SessionActiveMilliseconds int64  `json:"session_active_milliseconds"`
		RuntimeCostUSD            string `json:"runtime_cost_usd"`
	}
	if json.Unmarshal(usage, &payload) != nil {
		return 0, ""
	}
	return payload.SessionActiveMilliseconds, payload.RuntimeCostUSD
}

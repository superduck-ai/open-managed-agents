package db

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strconv"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/yourbatis"
)

type sessionUsageEvent struct {
	PriceSnapshot *billing.ModelPrice  `json:"_model_price"`
	Model         string               `json:"model"`
	Usage         *billing.Usage       `json:"model_usage"`
	LegacyUsage   *billing.Usage       `json:"usage"`
	IsError       bool                 `json:"is_error"`
	Billing       *sessionUsageBilling `json:"billing"`
}

type sessionUsageBilling struct {
	ListCost string `json:"list_cost"`
	Currency string `json:"currency"`
}

func insertSessionUsageRecord(ctx context.Context, executor yourbatis.Executor, event SessionEvent) error {
	if event.EventType != "span.model_request_end" {
		return nil
	}
	record, err := sessionUsageRecord(event)
	if err != nil {
		return err
	}
	return NewUsageRecordMapper(executor).Insert(ctx, record)
}

func sessionUsageRecord(event SessionEvent) (UsageRecord, error) {
	record := UsageRecord{Finalized: true, OrganizationUUID: event.OrganizationUUID, WorkspaceUUID: event.WorkspaceUUID,
		AttemptID: event.ExternalID, Source: "managed_agents", SessionUUID: new(event.SessionUUID),
		SessionExternalID: event.SessionExternalID, StartedAt: event.CreatedAt, EndedAt: event.CreatedAt,
		Version: "request_cents_v1"}
	var payload sessionUsageEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return UsageRecord{}, fmt.Errorf("decode session usage event: %w", err)
	}
	record.Model = payload.Model
	if payload.PriceSnapshot != nil {
		var err error
		record.PriceSnapshot, err = json.Marshal(payload.PriceSnapshot)
		if err != nil {
			return UsageRecord{}, fmt.Errorf("encode session usage price: %w", err)
		}
	}
	usage := payload.Usage
	if usage == nil {
		usage = payload.LegacyUsage
	}
	if usage != nil {
		ApplyUsage(&record, *usage)
		record.Complete = usage.Complete() && !payload.IsError
	}
	if payload.PriceSnapshot != nil {
		calculator := billing.NewCalculator(map[string]billing.ModelPrice{record.Model: *payload.PriceSnapshot})
		if usage != nil && usage.InputTokens != nil {
			if cents, ok := calculator.ModelRequestCents(record.Model, usage.Tokens()); ok {
				record.ModelCents = new(cents)
			}
		}
	} else if payload.Billing != nil && payload.Billing.Currency == "USD" {
		cents, err := strconv.ParseInt(payload.Billing.ListCost, 10, 64)
		if err == nil && cents >= 0 {
			record.ModelCents = new(cents)
		}
	}
	return record, nil
}

func ApplyUsage(record *UsageRecord, usage billing.Usage) {
	tokens := usage.Tokens()
	record.InputTokens, record.OutputTokens = tokens.InputTokens, tokens.OutputTokens
	record.CacheReadTokens, record.CacheWriteTokens = tokens.CacheReadInputTokens, tokens.CacheCreationInputTokens
	if usage.CacheCreation != nil {
		record.CacheWrite5mTokens = billing.UsageCount(usage.CacheCreation.FiveMinutes)
		record.CacheWrite1hTokens = billing.UsageCount(usage.CacheCreation.OneHour)
	}
	record.SearchRequests = usage.Searches()
	record.SearchCents = record.SearchRequests * billing.WebSearchCentsPerRequest
}

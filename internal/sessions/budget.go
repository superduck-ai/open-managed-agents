package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

// settlementEventTypes are the only client events accepted once a session is
// at or over its budget: they settle work already in progress and never start
// a new model request.
var settlementEventTypes = map[string]bool{
	"user.tool_confirmation":  true,
	"user.tool_result":        true,
	"user.custom_tool_result": true,
	"user.interrupt":          true,
}

// ParseBudgetInput validates a raw budget object from a create/update request.
// A JSON null means "remove the budget"; an absent field is represented by an
// empty raw value.
func ParseBudgetInput(raw json.RawMessage) (*billing.Budget, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if string(trimmed) == "null" {
		return nil, true, nil
	}
	budget, err := billing.ParseBudget(trimmed)
	if err != nil {
		return nil, false, err
	}
	return &budget, false, nil
}

// budgetGate rejects client events that would start new work on a paused
// session, listing the settlement events that remain accepted.
func budgetGate(eventType string) error {
	if settlementEventTypes[eventType] {
		return nil
	}
	return apperr.New(apperr.InvalidArgument,
		"session budget reached: only settlement events (user.tool_confirmation, user.tool_result, user.custom_tool_result, user.interrupt) are accepted",
		errors.New("session budget reached"),
	)
}

// sessionUsageJSON renders the contract-shaped usage object for a session or
// thread. budget is echoed verbatim (null when absent).
func sessionUsageJSON(totals db.SessionUsageTotals, budget json.RawMessage) json.RawMessage {
	totalCents := billing.TotalListCostCents(totals.ListCostCents, totals.WebSearchRequests, totals.ActiveSeconds)
	payload := map[string]any{
		"input_tokens":            totals.InputTokens,
		"output_tokens":           totals.OutputTokens,
		"cache_read_input_tokens": totals.CacheReadInputTokens,
		"cache_creation": map[string]any{
			"ephemeral_5m_input_tokens": totals.CacheCreationInputTokens,
			"ephemeral_1h_input_tokens": 0,
		},
		"list_cost":      billing.CostAmount{Amount: totalCents, Currency: billing.CurrencyUSD},
		"active_seconds": totals.ActiveSeconds,
		"server_tool_use": map[string]any{
			"web_search_requests": totals.WebSearchRequests,
			"web_fetch_requests":  0,
		},
	}
	if len(budget) > 0 && string(budget) != "null" {
		payload["budget"] = json.RawMessage(budget)
	} else {
		payload["budget"] = nil
	}
	raw, err := httpapi.MarshalRaw(payload)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// filterEventsAtBudgetCap enforces the settlement-only admission rule for a
// session paused at its budget. Non-settlement events are rejected with a 400
// listing the accepted settlement events; user.interrupt is accepted and
// ignored (dropped).
func filterEventsAtBudgetCap(inputs []json.RawMessage) ([]json.RawMessage, error) {
	filtered := make([]json.RawMessage, 0, len(inputs))
	for _, raw := range inputs {
		var probe map[string]any
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, invalidRequest(errors.New("event must be an object"))
		}
		eventType, _ := probe["type"].(string)
		if !settlementEventTypes[eventType] {
			return nil, budgetGate(eventType)
		}
		if eventType == "user.interrupt" {
			continue
		}
		filtered = append(filtered, raw)
	}
	return filtered, nil
}

// sessionBudgetAmount parses the stored budget, returning (0, false) when unset.
func sessionBudgetAmount(session db.Session) (int64, bool) {
	if len(session.Budget) == 0 {
		return 0, false
	}
	budget, err := billing.ParseBudget(session.Budget)
	if err != nil {
		return 0, false
	}
	return budget.MaxListCost.Amount, true
}

// enforceBudgetAfterEvents is called after a worker batch is persisted. It
// refreshes the session usage projection and, when a budgeted session first
// reaches its cap, marks the cap reached and emits the CMA budget_reached
// event sequence: session.usage immediately followed by session.status_idle
// with stop_reason budget_reached.
func (h *Handler) enforceBudgetAfterEvents(ctx context.Context, session db.Session, events []db.SessionEvent) {
	if h == nil || len(events) == 0 {
		return
	}
	interesting := false
	for _, event := range events {
		if event.EventType == "span.model_request_end" || event.EventType == "session.status_idle" || event.EventType == "session.thread_status_idle" {
			interesting = true
			break
		}
	}
	if !interesting {
		return
	}
	totals, err := h.db.SumSessionUsageTotals(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		h.logger.ErrorContext(ctx, "sum session usage", "session_id", session.ExternalID, "error", err)
		return
	}
	usage := sessionUsageJSON(totals, session.Budget)
	if err := h.db.SetSessionUsage(ctx, session.WorkspaceUUID, session.ExternalID, usage); err != nil {
		h.logger.ErrorContext(ctx, "set session usage", "session_id", session.ExternalID, "error", err)
	}
	budgetAmount, budgeted := sessionBudgetAmount(session)
	if !budgeted || session.BudgetReachedAt != nil {
		return
	}
	if billing.TotalListCostCents(totals.ListCostCents, totals.WebSearchRequests, totals.ActiveSeconds) < budgetAmount {
		return
	}
	now := time.Now().UTC()
	reached, err := h.db.MarkSessionBudgetReached(ctx, session.WorkspaceUUID, session.ExternalID, now)
	if err != nil {
		h.logger.ErrorContext(ctx, "mark session budget reached", "session_id", session.ExternalID, "error", err)
		return
	}
	if !reached {
		return
	}
	usageEvent := h.sessionUsageEvent(session, usage, now)
	idleEvent := h.budgetReachedIdleEvent(session, now)
	created, err := h.eventPayloads.AppendSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{usageEvent, idleEvent}, nil)
	if err != nil {
		h.logger.ErrorContext(ctx, "append budget reached events", "session_id", session.ExternalID, "error", err)
		return
	}
	h.publishSessionEvents(ctx, created)
}

// sessionUsageEvent builds the session.usage snapshot event.
func (h *Handler) sessionUsageEvent(session db.Session, usage json.RawMessage, now time.Time) db.SessionEvent {
	eventID, _ := ids.New("sevt_")
	payload, err := httpapi.MarshalRaw(map[string]any{
		"id":           eventID,
		"type":         "session.usage",
		"usage":        json.RawMessage(usage),
		"budget":       budgetJSONOrNull(session.Budget),
		"created_at":   httpapi.FormatTime(now),
		"processed_at": now.Format(time.RFC3339),
	})
	if err != nil {
		payload = json.RawMessage(`{"type":"session.usage"}`)
	}
	return db.SessionEvent{
		UUID:              uuid.NewV4().String(),
		ExternalID:        eventID,
		OrganizationUUID:  session.OrganizationUUID,
		WorkspaceUUID:     session.WorkspaceUUID,
		SessionUUID:       session.UUID,
		SessionExternalID: session.ExternalID,
		EventType:         "session.usage",
		Payload:           payload,
		ProcessedAt:       now,
		CreatedAt:         now,
	}
}

// budgetReachedIdleEvent builds the terminal session.status_idle event with
// stop_reason budget_reached.
func (h *Handler) budgetReachedIdleEvent(session db.Session, now time.Time) db.SessionEvent {
	eventID, _ := ids.New("sevt_")
	payload, err := httpapi.MarshalRaw(map[string]any{
		"id":           eventID,
		"type":         "session.status_idle",
		"stop_reason":  map[string]any{"type": "budget_reached"},
		"created_at":   httpapi.FormatTime(now),
		"processed_at": now.Format(time.RFC3339),
	})
	if err != nil {
		payload = json.RawMessage(`{"type":"session.status_idle"}`)
	}
	return db.SessionEvent{
		UUID:              uuid.NewV4().String(),
		ExternalID:        eventID,
		OrganizationUUID:  session.OrganizationUUID,
		WorkspaceUUID:     session.WorkspaceUUID,
		SessionUUID:       session.UUID,
		SessionExternalID: session.ExternalID,
		EventType:         "session.status_idle",
		Payload:           payload,
		ProcessedAt:       now,
		CreatedAt:         now,
	}
}

func budgetJSONOrNull(budget json.RawMessage) json.RawMessage {
	if len(budget) == 0 {
		return json.RawMessage("null")
	}
	return budget
}

// unpricedAgentModels walks an agent snapshot and returns the sorted set of
// model names referenced by "model" fields, for create-time pricing checks.
func unpricedAgentModels(snapshot json.RawMessage, calculator *billing.Calculator) []string {
	if len(snapshot) == 0 || calculator == nil {
		return nil
	}
	var tree any
	if err := json.Unmarshal(snapshot, &tree); err != nil {
		return nil
	}
	seen := map[string]bool{}
	collectModels(tree, seen)
	if len(seen) == 0 {
		return nil
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return calculator.UnpricedModels(models)
}

func collectModels(node any, seen map[string]bool) {
	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if key == "model" {
				if text, ok := value.(string); ok && text != "" {
					seen[text] = true
				}
			}
			collectModels(value, seen)
		}
	case []any:
		for _, item := range typed {
			collectModels(item, seen)
		}
	}
}

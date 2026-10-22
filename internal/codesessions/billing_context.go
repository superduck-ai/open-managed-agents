package codesessions

import (
	"context"
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var ErrModelPriceUnavailable = errors.New("model price unavailable for budgeted session")

type runtimeBilling struct {
	Budget            *billing.Budget                `json:"budget"`
	Usage             billing.CostAmount             `json:"usage"`
	Remaining         *billing.CostAmount            `json:"remaining"`
	BudgetReached     bool                           `json:"budget_reached"`
	ObservedAt        time.Time                      `json:"observed_at"`
	Pricing           map[string]*billing.ModelPrice `json:"pricing"`
	CalculationPolicy string                         `json:"calculation_policy"`
}

func (s *Service) billingContext(ctx context.Context, record db.CodeSession) (*runtimeBilling, error) {
	session, found, err := s.db.GetSession(ctx, record.WorkspaceUUID, record.SessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	totals, err := s.db.SumSessionUsageTotals(ctx, record.WorkspaceUUID, record.SessionExternalID)
	if err != nil {
		return nil, err
	}
	var metadata struct {
		Config managedAgentRuntimeConfigSchema `json:"config"`
	}
	if len(record.Metadata) > 0 {
		if err := json.Unmarshal(record.Metadata, &metadata); err != nil {
			return nil, err
		}
	}
	models := []string{firstNonEmpty(record.Model, metadata.Config.Model)}
	if metadata.Config.Subagents != nil {
		for _, agent := range metadata.Config.Subagents.Agents {
			if agent.Model != "" {
				models = append(models, agent.Model)
			}
		}
	}
	calculator, err := s.billing.Scope(ctx, record.OrganizationUUID, record.WorkspaceUUID, models...)
	if err != nil {
		return nil, err
	}
	return makeRuntimeBilling(session, totals, models, calculator)
}

func makeRuntimeBilling(session db.Session, totals db.SessionUsageTotals, models []string, calculator *billing.Calculator) (*runtimeBilling, error) {
	cost := billing.TotalListCostCents(totals.ListCostCents, totals.WebSearchRequests, totals.ActiveSeconds)
	result := &runtimeBilling{
		Usage:         billing.CostAmount{Amount: cost, Currency: billing.CurrencyUSD},
		BudgetReached: session.BudgetReachedAt != nil,
		ObservedAt:    time.Now().UTC(), Pricing: make(map[string]*billing.ModelPrice),
		CalculationPolicy: "server_authoritative_per_request_usd_cents;unpriced_usage_excluded;budget_missing_cache_rate_falls_back_to_input",
	}
	if len(session.Budget) > 0 {
		budget, err := billing.ParseBudget(session.Budget)
		if err != nil {
			return nil, err
		}
		result.Budget = &budget
		result.Remaining = &billing.CostAmount{Amount: max(0, budget.MaxListCost.Amount-cost), Currency: billing.CurrencyUSD}
	}
	for _, model := range models {
		if model != "" {
			result.Pricing[model] = calculator.PriceSnapshot(model)
		}
	}
	return result, nil
}

package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
)

type sessionBudgetLimit struct {
	Type        string              `json:"type"`
	MaxListCost sessionBudgetAmount `json:"max_list_cost"`
}

type sessionBudgetAmount struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func rejectUnpricedSessionBudget(raw, modelRaw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var budget sessionBudgetLimit
	if err := json.Unmarshal(raw, &budget); err != nil {
		return errors.New("budget must be a limit object with a USD max_list_cost")
	}
	if budget.Type != "limit" || budget.MaxListCost.Currency != "USD" || !positiveBudgetCents(budget.MaxListCost.Amount) {
		return errors.New("budget must have type limit and max_list_cost with a positive integer amount in USD cents")
	}
	var model struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(modelRaw, &model); err != nil {
		return errors.New("could not resolve budget model")
	}
	return fmt.Errorf("model_not_budgetable: no configured list price is available for model %q", model.ID)
}

func positiveBudgetCents(amount string) bool {
	if amount == "" || amount[0] == '0' {
		return false
	}
	for _, digit := range amount {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

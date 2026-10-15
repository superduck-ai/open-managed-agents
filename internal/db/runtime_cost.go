package db

import (
	"context"
	"encoding/json"
	"math/big"
	"slices"
	"time"
)

const (
	RuntimeCostGroupModel          = "model"
	RuntimeCostGroupWorkspace      = "workspace"
	RuntimeCostGroupServiceAccount = "service_account"
	runtimeCostMaxDays             = 31
)

type RuntimeCostQuery struct {
	OrganizationUUID    string
	WorkspaceExternalID string
	Model               string
	ServiceAccountID    string
	GroupBy             string
	StartingAt          time.Time
	EndingBefore        time.Time
	USDPerHour          *big.Rat
}

type RuntimeCostGroup struct {
	ID     string
	Label  string
	Amount string
}

type RuntimeCostDay struct {
	Date   string
	Amount string
	Groups []RuntimeCostGroup
}

type RuntimeCostFacet struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type RuntimeCost struct {
	Currency        string
	Total           string
	Daily           []RuntimeCostDay
	Groups          []RuntimeCostGroup
	Models          []string
	ServiceAccounts []RuntimeCostFacet
}

func (d *DB) RuntimeCost(ctx context.Context, query RuntimeCostQuery) (RuntimeCost, error) {
	if err := validateRuntimeCostQuery(query); err != nil {
		return RuntimeCost{}, err
	}
	points, err := NewSessionEventMapper(d.mapperDB).ListRuntimeUsagePoints(ctx, runtimeUsageQuery{
		OrganizationUUID:    query.OrganizationUUID,
		WorkspaceExternalID: query.WorkspaceExternalID,
		StartingAt:          query.StartingAt,
		EndingBefore:        query.EndingBefore,
	})
	if err != nil {
		return RuntimeCost{}, err
	}
	return aggregateRuntimeUsage(points, query), nil
}

func validateRuntimeCostQuery(query RuntimeCostQuery) error {
	if query.OrganizationUUID == "" || query.StartingAt.IsZero() || !query.EndingBefore.After(query.StartingAt) {
		return ErrInvalidState
	}
	if query.EndingBefore.Sub(query.StartingAt) > runtimeCostMaxDays*24*time.Hour {
		return ErrInvalidState
	}
	if query.USDPerHour == nil || query.USDPerHour.Sign() <= 0 {
		return ErrInvalidState
	}
	switch query.GroupBy {
	case "", RuntimeCostGroupModel, RuntimeCostGroupWorkspace, RuntimeCostGroupServiceAccount:
		return nil
	default:
		return ErrInvalidState
	}
}

func aggregateRuntimeUsage(points []runtimeUsagePointRow, query RuntimeCostQuery) RuntimeCost {
	groupBy := query.GroupBy
	if groupBy == "" {
		groupBy = RuntimeCostGroupModel
	}
	facets := runtimeCostFacets(points, query.StartingAt)
	costByDay := map[string]*big.Rat{}
	costByDayGroup := map[string]map[string]*big.Rat{}
	labels := map[string]string{}
	costByGroup := map[string]*big.Rat{}
	var previousSession string
	previousCost := new(big.Rat)
	for _, point := range points {
		cost := snapshotRuntimeCost(point, query.USDPerHour)
		if point.SessionUUID != previousSession {
			previousSession = point.SessionUUID
			previousCost = new(big.Rat)
		}
		delta := new(big.Rat).Sub(cost, previousCost)
		previousCost = cost
		if point.ProcessedAt.Before(query.StartingAt) || delta.Sign() <= 0 {
			continue
		}
		if query.Model != "" && point.Model != query.Model {
			continue
		}
		if query.ServiceAccountID != "" && point.ServiceAccountID != query.ServiceAccountID {
			continue
		}
		day := point.ProcessedAt.UTC().Format("2006-01-02")
		addRat(costByDay, day, delta)
		id, label := runtimeCostGroup(point, groupBy)
		labels[id] = label
		addRat(costByGroup, id, delta)
		if costByDayGroup[day] == nil {
			costByDayGroup[day] = map[string]*big.Rat{}
		}
		addRat(costByDayGroup[day], id, delta)
	}
	return RuntimeCost{
		Currency:        "USD",
		Total:           formatUSD(sumRats(costByDay)),
		Daily:           runtimeCostDays(query.StartingAt, query.EndingBefore, costByDay, costByDayGroup, labels),
		Groups:          runtimeCostGroups(costByGroup, labels),
		Models:          facets.models,
		ServiceAccounts: facets.accounts,
	}
}

type runtimeFacets struct {
	models   []string
	accounts []RuntimeCostFacet
}

func runtimeCostFacets(points []runtimeUsagePointRow, startingAt time.Time) runtimeFacets {
	models := map[string]struct{}{}
	accounts := map[string]string{}
	for _, point := range points {
		if point.ProcessedAt.Before(startingAt) {
			continue
		}
		if point.Model != "" {
			models[point.Model] = struct{}{}
		}
		if point.ServiceAccountID != "" {
			accounts[point.ServiceAccountID] = point.ServiceAccountName
		}
	}
	modelList := make([]string, 0, len(models))
	for model := range models {
		modelList = append(modelList, model)
	}
	slices.Sort(modelList)
	accountList := make([]RuntimeCostFacet, 0, len(accounts))
	for id, label := range accounts {
		accountList = append(accountList, RuntimeCostFacet{ID: id, Label: label})
	}
	slices.SortFunc(accountList, func(left, right RuntimeCostFacet) int {
		if left.Label == right.Label {
			if left.ID < right.ID {
				return -1
			}
			if left.ID > right.ID {
				return 1
			}
			return 0
		}
		if left.Label < right.Label {
			return -1
		}
		return 1
	})
	return runtimeFacets{models: modelList, accounts: accountList}
}

func runtimeCostGroup(point runtimeUsagePointRow, groupBy string) (string, string) {
	switch groupBy {
	case RuntimeCostGroupWorkspace:
		return point.WorkspaceExternalID, point.WorkspaceName
	case RuntimeCostGroupServiceAccount:
		if point.ServiceAccountID == "" {
			return "", "unknown"
		}
		label := point.ServiceAccountName
		if label == "" {
			label = point.ServiceAccountID
		}
		return point.ServiceAccountID, label
	default:
		if point.Model == "" {
			return "", "unknown"
		}
		return point.Model, point.Model
	}
}

func runtimeCostDays(start, end time.Time, byDay map[string]*big.Rat, byDayGroup map[string]map[string]*big.Rat, labels map[string]string) []RuntimeCostDay {
	days := []RuntimeCostDay{}
	for day := start.UTC(); day.Before(end.UTC()); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		days = append(days, RuntimeCostDay{
			Date:   key,
			Amount: formatUSD(byDay[key]),
			Groups: runtimeCostGroups(byDayGroup[key], labels),
		})
	}
	return days
}

func runtimeCostGroups(costs map[string]*big.Rat, labels map[string]string) []RuntimeCostGroup {
	ids := make([]string, 0, len(costs))
	for id := range costs {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(left, right string) int {
		compare := costs[right].Cmp(costs[left])
		if compare != 0 {
			return compare
		}
		if labels[left] < labels[right] {
			return -1
		}
		if labels[left] > labels[right] {
			return 1
		}
		return 0
	})
	groups := make([]RuntimeCostGroup, 0, len(ids))
	for _, id := range ids {
		label := labels[id]
		if label == "" {
			label = id
		}
		groups = append(groups, RuntimeCostGroup{ID: id, Label: label, Amount: formatUSD(costs[id])})
	}
	return groups
}

func addRat(target map[string]*big.Rat, key string, delta *big.Rat) {
	current := target[key]
	if current == nil {
		current = new(big.Rat)
	}
	target[key] = new(big.Rat).Add(current, delta)
}

func sumRats(values map[string]*big.Rat) *big.Rat {
	total := new(big.Rat)
	for _, value := range values {
		total.Add(total, value)
	}
	return total
}

func runtimeUSD(millis int64, usdPerHour *big.Rat) *big.Rat {
	if millis <= 0 || usdPerHour == nil || usdPerHour.Sign() <= 0 {
		return new(big.Rat)
	}
	cost := new(big.Rat).Mul(big.NewRat(millis, 1), usdPerHour)
	return cost.Quo(cost, big.NewRat(3600000, 1))
}

func snapshotRuntimeCost(point runtimeUsagePointRow, usdPerHour *big.Rat) *big.Rat {
	if point.RuntimeCostUSD != "" {
		parsed := new(big.Rat)
		if _, ok := parsed.SetString(point.RuntimeCostUSD); ok {
			return parsed
		}
	}
	millis := new(big.Rat)
	if _, ok := millis.SetString(point.ActiveMilliseconds); !ok {
		return new(big.Rat)
	}
	millis.Mul(millis, usdPerHour)
	return millis.Quo(millis, big.NewRat(3600000, 1))
}

func addRuntimeCostUSD(usage json.RawMessage, addedMillis int64, usdPerHour *big.Rat) (json.RawMessage, error) {
	if usdPerHour == nil || usdPerHour.Sign() <= 0 {
		return nil, ErrInvalidState
	}
	payload := map[string]json.RawMessage{}
	if len(usage) > 0 {
		if err := json.Unmarshal(usage, &payload); err != nil {
			return nil, err
		}
	}
	var totalMillis int64
	if raw := payload["active_milliseconds"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &totalMillis); err != nil {
			return nil, err
		}
	}
	priorMillis := totalMillis - addedMillis
	if priorMillis < 0 {
		priorMillis = 0
	}
	cost := new(big.Rat)
	if raw := payload["runtime_cost_usd"]; len(raw) > 0 {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		if _, ok := cost.SetString(text); !ok {
			return nil, ErrInvalidState
		}
	} else {
		cost = runtimeUSD(priorMillis, usdPerHour)
	}
	cost.Add(cost, runtimeUSD(addedMillis, usdPerHour))
	encoded, err := json.Marshal(formatUSD(cost))
	if err != nil {
		return nil, err
	}
	payload["runtime_cost_usd"] = encoded
	return json.Marshal(payload)
}

func FormatDecimalAmount(value *big.Rat) string {
	return formatUSD(value)
}

func formatUSD(cost *big.Rat) string {
	if cost == nil || cost.Sign() <= 0 {
		return "0.00"
	}
	scaled := new(big.Rat).Mul(cost, big.NewRat(1000000, 1))
	scaled.Add(scaled, big.NewRat(1, 2))
	digits := new(big.Int).Quo(scaled.Num(), scaled.Denom()).String()
	for len(digits) < 7 {
		digits = "0" + digits
	}
	whole := digits[:len(digits)-6]
	frac := digits[len(digits)-6:]
	for len(frac) > 2 && frac[len(frac)-1] == '0' {
		frac = frac[:len(frac)-1]
	}
	return whole + "." + frac
}

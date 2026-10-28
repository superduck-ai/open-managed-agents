package db

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestAggregateRuntimeUsage(t *testing.T) {
	start := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 2)
	rate, err := config.RuntimeCostConfig{USDPerHour: "0.01652866"}.HourlyUSD()
	if err != nil {
		t.Fatal(err)
	}
	points := []runtimeUsagePointRow{
		{SessionUUID: "s1", ProcessedAt: start.Add(-time.Hour), ActiveMilliseconds: "10000", Model: "old", WorkspaceExternalID: "default", WorkspaceName: "Default"},
		{SessionUUID: "s1", ProcessedAt: start.Add(time.Hour), ActiveMilliseconds: "11500", Model: "claude-opus-4-6", WorkspaceExternalID: "default", WorkspaceName: "Default"},
		{SessionUUID: "s1", ProcessedAt: start.Add(30 * time.Hour), ActiveMilliseconds: "11500", Model: "claude-opus-4-6", WorkspaceExternalID: "default", WorkspaceName: "Default"},
		{SessionUUID: "s2", ProcessedAt: start.Add(2 * time.Hour), ActiveMilliseconds: "48073", Model: "claude-haiku-4-5", WorkspaceExternalID: "extra", WorkspaceName: "Extra"},
	}
	cost := aggregateRuntimeUsage(points, RuntimeCostQuery{StartingAt: start, EndingBefore: end, GroupBy: RuntimeCostGroupModel, USDPerHour: rate})
	if cost.Currency != "USD" || cost.Total != "0.000228" {
		t.Fatalf("total = %s %s", cost.Currency, cost.Total)
	}
	if len(cost.Daily) != 2 || cost.Daily[0].Amount != "0.000228" || cost.Daily[1].Amount != "0.00" {
		t.Fatalf("daily = %+v", cost.Daily)
	}
	if len(cost.Groups) != 2 || cost.Groups[0].ID != "claude-haiku-4-5" {
		t.Fatalf("groups = %+v", cost.Groups)
	}
	filtered := aggregateRuntimeUsage(points, RuntimeCostQuery{StartingAt: start, EndingBefore: end, Model: "claude-opus-4-6", USDPerHour: rate})
	if filtered.Total != "0.000007" {
		t.Fatalf("filtered total = %s", filtered.Total)
	}
	if got := formatUSD(runtimeUSD(48073, rate)); got != "0.000221" {
		t.Fatalf("48073ms = %s", got)
	}
	custom := new(big.Rat).Quo(big.NewRat(1, 5), big.NewRat(7, 1))
	if got := formatUSD(runtimeUSD(3600000, custom)); got != "0.028571" {
		t.Fatalf("custom rate = %s", got)
	}
	frozen := aggregateRuntimeUsage([]runtimeUsagePointRow{{
		SessionUUID: "s", ProcessedAt: start.Add(time.Hour), ActiveMilliseconds: "999999999", RuntimeCostUSD: "1/100",
		Model: "m", WorkspaceExternalID: "default", WorkspaceName: "Default",
	}}, RuntimeCostQuery{StartingAt: start, EndingBefore: end, USDPerHour: rate})
	if frozen.Total != "0.01" {
		t.Fatalf("stored cost = %s", frozen.Total)
	}
}

func TestAddRuntimeCostUSDStoresDecimal(t *testing.T) {
	rate, err := config.RuntimeCostConfig{USDPerHour: "0.01652866"}.HourlyUSD()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := addRuntimeCostUSD([]byte(`{"session_active_milliseconds":8367}`), 8367, rate)
	if err != nil {
		t.Fatal(err)
	}
	var usage struct {
		Cost string `json:"runtime_cost_usd"`
	}
	if err := json.Unmarshal(raw, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.Cost != "0.000038" {
		t.Fatalf("runtime_cost_usd = %s", usage.Cost)
	}
}

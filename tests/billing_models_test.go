package tests

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/analytics"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/metering"
	"github.com/superduck-ai/open-managed-agents/internal/pricing"
)

type billingRecordedStore struct {
	database *db.DB
	records  []db.UsageRecord
}

func (s *billingRecordedStore) InsertUsageRecord(ctx context.Context, record db.UsageRecord) error {
	if err := s.database.InsertUsageRecord(ctx, record); err != nil {
		return err
	}
	s.records = append(s.records, record)
	return nil
}

func TestNarrowsModelsOpenRouterReconciliation(t *testing.T) {
	raw, err := os.ReadFile("testdata/billing/narrows-openrouter.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Model       string         `json:"model"`
			SourceModel string         `json:"source_model"`
			TotalInput  int64          `json:"total_input"`
			Usage       jsontext.Value `json:"usage"`
			Cents       *int64         `json:"cents"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	prices, err := pricing.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	app := newTestAppWithStore(t, nil, newFakeStore("多模型计费对账"))
	defer app.close()
	models := []string{}
	seen := map[string]bool{}
	for _, tc := range fixture.Cases {
		if !seen[tc.Model] {
			models = append(models, tc.Model)
			seen[tc.Model] = true
		}
	}
	if len(models) != 61 || len(prices) != 32 {
		t.Fatalf("模型覆盖变化：%d/%d", len(models), len(prices))
	}
	provider := seedTestLLMProviderForWorkspace(t, app, uuid.NewV4().String(), uuid.NewV4().String(), "多模型计费对账", "https://example.test", "fixture-key", models...)
	if err := app.db.ReplaceModelPriceCatalog(t.Context(), "openrouter", prices); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(fmt.Sprintf("%s/%d", tc.Model, tc.TotalInput), func(t *testing.T) {
			usageJSON, err := json.Marshal(tc.Usage)
			if err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("stream") == "true" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":%s}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":2345}}\n\ndata: {\"type\":\"message_stop\"}\n\n", usageJSON)
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"type":"message","usage":%s}`, usageJSON)
				}
			}))
			defer upstream.Close()
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
					store := &billingRecordedStore{database: app.db}
					transport := &metering.Transport{Base: http.DefaultTransport, Store: store, Calculator: app.db.BillingCalculator(), Source: "api"}
					ctx := auth.WithPrincipal(t.Context(), auth.Principal{OrganizationUUID: provider.OrganizationUUID, WorkspaceUUID: provider.WorkspaceUUID})
					request, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s?stream=%t", upstream.URL, stream), strings.NewReader(fmt.Sprintf(`{"model":%q}`, tc.Model)))
					if err != nil {
						t.Fatal(err)
					}
					response, err := transport.RoundTrip(request)
					if err != nil {
						t.Fatal(err)
					}
					_, err = io.Copy(io.Discard, response.Body)
					closeErr := response.Body.Close()
					if err != nil || closeErr != nil {
						t.Fatalf("读取响应：%v/%v", err, closeErr)
					}
					if len(store.records) != 2 {
						t.Fatalf("落账次数：%d", len(store.records))
					}
					first, last := store.records[0], store.records[1]
					if first.Finalized || !last.Finalized || !last.Complete || first.AttemptID != last.AttemptID {
						t.Fatal("请求结算状态错误")
					}
					if (last.ModelCents == nil) != (tc.Cents == nil) || (tc.Cents != nil && *last.ModelCents != *tc.Cents) {
						t.Fatalf("模型=%s 金额=%v，预期=%v", tc.Model, last.ModelCents, tc.Cents)
					}
					if tc.SourceModel != "" {
						var snapshot billing.ModelPrice
						if err := json.Unmarshal(last.PriceSnapshot, &snapshot); err != nil {
							t.Fatal(err)
						}
						if snapshot.Source != "openrouter" || snapshot.SourceModel != tc.SourceModel || snapshot.ProviderUUID != provider.UUID || snapshot.Version == "" || string(first.PriceSnapshot) != string(last.PriceSnapshot) {
							t.Fatalf("价格快照匹配错误：%+v", snapshot)
						}
					} else if len(last.PriceSnapshot) != 0 {
						t.Fatal("未匹配模型错误绑定价格")
					}
					var usage billing.Usage
					if err := json.Unmarshal(tc.Usage, &usage); err != nil {
						t.Fatal(err)
					}
					if last.InputTokens != *usage.InputTokens || last.OutputTokens != *usage.OutputTokens || last.CacheReadTokens != *usage.CacheReadTokens || last.CacheWriteTokens != *usage.CacheWriteTokens {
						t.Fatalf("响应用量落账错误：%+v", last)
					}
					var databaseNow time.Time
					if err := app.db.SQLDB().QueryRowContext(t.Context(), "SELECT CURRENT_TIMESTAMP").Scan(&databaseNow); err != nil {
						t.Fatal(err)
					}
					query := db.UsageQuery{OrganizationUUID: provider.OrganizationUUID, WorkspaceUUIDs: []string{provider.WorkspaceUUID}, Model: tc.Model, Start: last.StartedAt, End: time.Now().UTC().Add(time.Second), AsOf: databaseNow, Timezone: "UTC"}
					rows, err := app.db.AggregateUsage(t.Context(), query)
					if err != nil {
						t.Fatal(err)
					}
					report := analytics.BuildReport(rows, query)
					want := analytics.Metrics{Requests: 1, InputTokens: last.InputTokens, OutputTokens: last.OutputTokens, CacheReadTokens: last.CacheReadTokens, CacheWriteTokens: last.CacheWriteTokens}
					if tc.Cents == nil {
						want.Unpriced = 1
					} else {
						want.ModelCents = *tc.Cents
						want.SubtotalCents = *tc.Cents
					}
					if report.Summary != want {
						t.Fatalf("汇总对账：%+v，预期：%+v", report.Summary, want)
					}
					var dailyRequests, dailyCents int64
					for _, day := range report.Daily {
						dailyRequests += day.Metrics.Requests
						dailyCents += day.Metrics.SubtotalCents
					}
					if dailyRequests != 1 || dailyCents != want.SubtotalCents {
						t.Fatal("每日费用与汇总不一致")
					}
				})
			}
		})
	}
}

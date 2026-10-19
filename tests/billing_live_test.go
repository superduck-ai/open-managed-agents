package tests

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type liveBillingResult struct {
	PriceSnapshot jsontext.Value `json:"price_snapshot,omitempty"`
	Model         string         `json:"model"`
	Status        string         `json:"status"`
	HTTPStatus    int            `json:"http_status"`
	Usage         billing.Usage  `json:"usage"`
	SourceModel   string         `json:"source_model,omitempty"`
	Cents         *int64         `json:"cents"`
	ExpectedCents *int64         `json:"expected_cents"`
	Detail        string         `json:"detail,omitempty"`
}

func TestLiveNarrowsBilling(t *testing.T) {
	base := os.Getenv("LIVE_BILLING_BASE_URL")
	if base == "" {
		t.Skip("未指定真实计费验收服务")
	}
	pricesPath, reportPath := os.Getenv("LIVE_BILLING_PRICES"), os.Getenv("LIVE_BILLING_REPORT")
	if pricesPath == "" || reportPath == "" {
		t.Fatal("需要价格快照与脱敏报告路径")
	}
	raw, err := os.ReadFile(pricesPath)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Data []struct {
			ID      string         `json:"id"`
			Pricing jsontext.Value `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	prices := map[string]jsontext.Value{}
	for _, p := range catalog.Data {
		prices[p.ID] = p.Pricing
	}
	if os.Getenv("LIVE_BILLING_SOURCE") == "narrows" {
		var catalog struct {
			Rules []jsontext.Value `json:"rules"`
		}
		if err := json.Unmarshal(raw, &catalog); err != nil {
			t.Fatal(err)
		}
		for _, rule := range catalog.Rules {
			var id struct {
				Model string `json:"model"`
			}
			if err := json.Unmarshal(rule, &id); err != nil {
				t.Fatal(err)
			}
			prices[id.Model] = rule
		}
	}
	selected := []string{}
	if models := os.Getenv("LIVE_BILLING_MODELS"); models != "" {
		for model := range strings.SplitSeq(models, ",") {
			if model = strings.TrimSpace(model); model == "" {
				t.Fatal("实测模型名称不能为空")
			}
			selected = append(selected, model)
		}
	} else if os.Getenv("LIVE_BILLING_SOURCE") == "narrows" {
		for model := range prices {
			selected = append(selected, model)
		}
	} else {
		t.Fatal("使用 OpenRouter 价格时需通过 LIVE_BILLING_MODELS 指定实测模型")
	}
	if len(selected) == 0 {
		t.Fatal("没有可实测的模型")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.OpenExisting(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	key := os.Getenv("LIVE_BILLING_API_KEY")
	if key == "" {
		key = config.DefaultAPIKey
	}
	identity, err := database.GetAPIKey(t.Context(), auth.HashAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	repeats, _ := strconv.Atoi(os.Getenv("LIVE_BILLING_INPUT_REPEATS"))
	if repeats < 0 || repeats > 30000 {
		t.Fatal("测试输入重复次数需在 0–30000 之间")
	}
	results := []liveBillingResult{}
	seen := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, model := range selected {
		if seen[model] {
			continue
		}
		seen[model] = true
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			result := checkLiveBilling(t.Context(), database, base, key, model, prices, identity, repeats)
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
			t.Logf("%s: %s (%d)", model, result.Status, result.HTTPStatus)
		})
	}
	wg.Wait()
	data, err := json.Marshal(results, jsontext.WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status == "mismatch" || r.Status == "blocked" {
			t.Errorf("%s: %s", r.Model, r.Detail)
		}
	}
}

func checkLiveBilling(ctx context.Context, database *db.DB, base, key, model string, prices map[string]jsontext.Value, identity db.APIKey, repeats int) liveBillingResult {
	result := liveBillingResult{Model: model, Status: "blocked"}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	started := time.Now().UTC()
	prompt := "Reply with only OK. " + strings.Repeat("billing ", repeats)
	body := fmt.Sprintf(`{"model":%q,"max_tokens":128,"messages":[{"role":"user","content":%q}]}`, model, prompt)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", strings.NewReader(body))
	if err != nil {
		result.Detail = "构造请求失败"
		return result
	}
	request.Header.Set("x-api-key", key)
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("content-type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		result.Detail = "请求失败或超时"
		return result
	}
	defer response.Body.Close()
	result.HTTPStatus = response.StatusCode
	if response.StatusCode != 200 {
		result.Detail = "上游或服务拒绝请求"
		return result
	}
	var message struct {
		Usage billing.Usage `json:"usage"`
	}
	if err := json.UnmarshalRead(io.LimitReader(response.Body, 4<<20), &message); err != nil {
		result.Detail = "响应解码失败"
		return result
	}
	result.Usage = message.Usage
	if !message.Usage.Complete() {
		result.Detail = "上游返回的用量不完整"
		return result
	}
	var record db.UsageRecord
	var snapshot []byte
	for {
		err = database.SQLDB().QueryRowContext(ctx, `SELECT input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,model_cents,price_snapshot,complete FROM usage_records WHERE model=$1 AND source='api' AND started_at >= $2 AND finalized AND organization_uuid=$3 AND workspace_uuid=$4 AND api_key_uuid=$5 ORDER BY started_at LIMIT 1`, model, started, identity.OrganizationUUID.String(), identity.WorkspaceUUID.String(), identity.UUID.String()).Scan(&record.InputTokens, &record.OutputTokens, &record.CacheReadTokens, &record.CacheWriteTokens, &record.CacheWrite5mTokens, &record.CacheWrite1hTokens, &record.ModelCents, &snapshot, &record.Complete)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			result.Status = "mismatch"
			result.Detail = "未找到结算记录"
			return result
		case <-time.After(50 * time.Millisecond):
		}
	}
	result.Cents = record.ModelCents
	result.PriceSnapshot = snapshot
	tokens := message.Usage.Tokens()
	if !record.Complete || record.InputTokens != tokens.InputTokens || record.OutputTokens != tokens.OutputTokens || record.CacheReadTokens != tokens.CacheReadInputTokens || record.CacheWriteTokens != tokens.CacheCreationInputTokens || record.CacheWrite5mTokens != tokens.CacheWrite5mTokens || record.CacheWrite1hTokens != tokens.CacheWrite1hTokens {
		result.Status = "mismatch"
		result.Detail = "响应用量与账本不一致"
		return result
	}
	if os.Getenv("LIVE_BILLING_SOURCE") == "narrows" {
		return reconcileNarrowsLive(result, prices[model], tokens)
	}
	var price billing.ModelPrice
	expectedSource := liveSourceModel(prices, model)
	if len(snapshot) == 0 || string(snapshot) == "null" {
		if expectedSource != "" {
			result.Status = "mismatch"
			result.Detail = "价格源有匹配模型但落账缺少快照"
			return result
		}
		if record.ModelCents != nil {
			result.Status = "mismatch"
			result.Detail = "缺少快照却产生费用"
		} else {
			result.Status = "unpriced"
			result.Detail = "用量已落账，无精确价格匹配"
		}
		return result
	}
	if json.Unmarshal(snapshot, &price) != nil || price.Source != "openrouter" {
		result.Status = "mismatch"
		result.Detail = "价格快照无效"
		return result
	}
	result.SourceModel = price.SourceModel
	if expectedSource != price.SourceModel {
		result.Status = "mismatch"
		result.Detail = "模型绑定了错误价格来源"
		return result
	}
	if !liveSnapshotRatesMatch(prices[price.SourceModel], price.Rates) {
		result.Status = "mismatch"
		result.Detail = "持久化单价与 OpenRouter 原始价格不一致"
		return result
	}
	expected, ok := livePriceOracle(prices[price.SourceModel], tokens)
	if !ok {
		result.Status = "blocked"
		result.Detail = "独立复算缺少价格或未覆盖该条件"
		return result
	}
	result.ExpectedCents = new(expected)
	if record.ModelCents == nil || *record.ModelCents != expected {
		result.Status = "mismatch"
		result.Detail = "OpenRouter 逐 Token 独立计算与落账不一致"
		return result
	}
	result.Status = "passed"
	return result
}

func livePriceOracle(raw jsontext.Value, u billing.TokenUsage) (int64, bool) {
	var p struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
		Read       string `json:"input_cache_read"`
		Write      string `json:"input_cache_write"`
		Overrides  []struct {
			Min int64 `json:"min_prompt_tokens"`
		} `json:"overrides"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return 0, false
	}
	for _, o := range p.Overrides {
		if u.InputTokens+u.CacheReadInputTokens+u.CacheCreationInputTokens > o.Min {
			return 0, false
		}
	}
	total := new(big.Rat)
	for _, term := range []struct {
		tokens int64
		rate   string
	}{{u.InputTokens, p.Prompt}, {u.OutputTokens, p.Completion}, {u.CacheReadInputTokens, p.Read}, {u.CacheCreationInputTokens, p.Write}} {
		if term.tokens == 0 {
			continue
		}
		rate, ok := new(big.Rat).SetString(term.rate)
		if !ok || rate.Sign() < 0 {
			return 0, false
		}
		total.Add(total, new(big.Rat).Mul(rate, new(big.Rat).SetInt64(term.tokens)))
	}
	total.Mul(total, big.NewRat(100, 1)).Add(total, big.NewRat(1, 2))
	rounded := new(big.Int).Quo(total.Num(), total.Denom())
	return rounded.Int64(), rounded.IsInt64()
}

func liveSourceModel(prices map[string]jsontext.Value, model string) string {
	if _, ok := prices[model]; ok {
		return model
	}
	match := ""
	for id := range prices {
		_, bare, found := strings.Cut(id, "/")
		if found && bare == model && !strings.HasPrefix(id, "~") {
			if match != "" {
				return ""
			}
			match = id
		}
	}
	return match
}

func liveSnapshotRatesMatch(raw jsontext.Value, rates *billing.Rates) bool {
	if rates == nil {
		return false
	}
	var source map[string]jsontext.Value
	if json.Unmarshal(raw, &source) != nil {
		return false
	}
	for _, term := range []struct {
		key  string
		rate *string
	}{
		{"prompt", rates.Input}, {"completion", rates.Output}, {"input_cache_read", rates.CacheRead}, {"input_cache_write", rates.CacheWrite},
	} {
		value, exists := source[term.key]
		if !exists {
			if term.rate != nil {
				return false
			}
			continue
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			return false
		}
		expected, ok := new(big.Rat).SetString(text)
		if !ok {
			return false
		}
		if expected.Sign() < 0 {
			if term.rate != nil {
				return false
			}
			continue
		}
		if term.rate == nil {
			return false
		}
		actual, ok := new(big.Rat).SetString(*term.rate)
		if !ok || expected.Mul(expected, big.NewRat(1000000, 1)).Cmp(actual) != 0 {
			return false
		}
	}
	return true
}

func TestLiveCacheUsageInvariant(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after billing.Usage
		mismatch      bool
	}{
		{"缓存读取替代普通输入", billing.Usage{InputTokens: new(int64(30089))}, billing.Usage{InputTokens: new(int64(393)), CacheReadTokens: new(int64(29696))}, false},
		{"缓存重复计入输入", billing.Usage{InputTokens: new(int64(30012))}, billing.Usage{InputTokens: new(int64(30012)), CacheReadTokens: new(int64(29824))}, true},
		{"未命中缓存", billing.Usage{InputTokens: new(int64(30006))}, billing.Usage{InputTokens: new(int64(30006))}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if liveCacheInputChanged(tc.before, tc.after) != tc.mismatch {
				t.Fatal("缓存输入口径检查错误")
			}
		})
	}
}

func liveCacheInputChanged(before, after billing.Usage) bool {
	if billing.UsageCount(after.CacheReadTokens) == 0 {
		return false
	}
	a, b := before.Tokens(), after.Tokens()
	return a.InputTokens+a.CacheReadInputTokens+a.CacheCreationInputTokens != b.InputTokens+b.CacheReadInputTokens+b.CacheCreationInputTokens
}

func TestLiveBillingCacheComparison(t *testing.T) {
	baseline, candidate := os.Getenv("LIVE_BILLING_CACHE_BASELINE"), os.Getenv("LIVE_BILLING_CACHE_CANDIDATE")
	if baseline == "" || candidate == "" {
		t.Skip("需要相同请求的两轮真实报告")
	}
	previous := map[string]liveBillingResult{}
	compared := 0
	for i, path := range []string{baseline, candidate} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var rows []liveBillingResult
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.Status != "passed" {
				continue
			}
			if i == 0 {
				previous[row.Model] = row
				continue
			}
			if before, ok := previous[row.Model]; ok {
				compared++
				if liveCacheInputChanged(before.Usage, row.Usage) {
					t.Errorf("%s：相同请求缓存命中后总输入变化，可能重复计费", row.Model)
				}
			}
		}
	}
	if compared == 0 {
		t.Fatal("没有可比较的成功请求")
	}
}

func TestLivePriceOracleCacheCost(t *testing.T) {
	raw := jsontext.Value(`{"prompt":"0.00000175","completion":"0.000014","input_cache_read":"0.000000175"}`)
	for _, tc := range []struct{ input, want int64 }{{188, 1}, {30012, 6}} {
		got, ok := livePriceOracle(raw, billing.TokenUsage{InputTokens: tc.input, OutputTokens: 4, CacheReadInputTokens: 29824})
		if !ok || got != tc.want {
			t.Fatalf("独立复算=%d，预期=%d", got, tc.want)
		}
	}
	if !liveSnapshotRatesMatch(raw, &billing.Rates{Input: new("1.75"), Output: new("14"), CacheRead: new("0.175")}) {
		t.Fatal("正确单位转换不匹配")
	}
	if liveSnapshotRatesMatch(raw, &billing.Rates{Input: new("1.75"), Output: new("14"), CacheRead: new("1.75")}) {
		t.Fatal("错误缓存单价未被发现")
	}
}

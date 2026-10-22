//go:build integration || e2e || e2b_integration

package tests

import (
	"encoding/json/v2"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/pricing"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/analytics"
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestAnalyticsMeteredRequests(t *testing.T) {
	model := "analytics-priced-" + uuid.NewV4().String()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.ReplaceAll(`{"id":"msg_usage","type":"message","role":"assistant","model":"analytics-priced-model","content":[{"type":"text","text":"已完成"}],"stop_reason":"end_turn","usage":{"input_tokens":1000,"output_tokens":1000,"cache_read_input_tokens":1000,"cache_creation_input_tokens":1000,"server_tool_use":{"web_search_requests":2}}}`, "analytics-priced-model", model))
	}))
	defer upstream.Close()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	app := newTestAppWithStore(t, &cfg, newFakeStore("analytics-requests"))
	defer app.close()
	provider := seedTestLLMProvider(t, app, "Analytics", upstream.URL, "analytics-key", model)
	qualifiedModel := llmproviders.QualifiedModelID(provider.Slug, model)
	if err := app.prices.Replace("openrouter", []pricing.CatalogPrice{{Model: model, Original: []byte(`{}`), Price: billing.ModelPrice{Source: "openrouter", SourceCurrency: "USD", Version: "fixture", SourceModel: model, Rates: &billing.Rates{Input: new("10"), Output: new("10"), CacheRead: new("10"), CacheWrite: new("10")}}}}); err != nil {
		t.Fatal(err)
	}

	org := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "analytics@example.com")
	prefix := "/api/organizations/" + org
	body := `{"model":"` + model + `","max_tokens":16,"messages":[{"role":"user","content":"测试"}]}`
	response := doMessagesRequest(t, app, defaultTestKey, body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("messages: %d %s", response.StatusCode, readAll(t, response.Body))
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	for _, request := range []struct{ path, body string }{
		{"/proxy/v1/messages", body},
		{"/workbench/completions", body},
		{"/workbench/generate_prompt", `{"model":"` + model + `","task":"测试提示词"}`},
		{"/workbench/generate_title", `{"model":"` + model + `","message_content":"测试标题"}`},
	} {
		response := app.platformRequest(t, http.MethodPost, prefix+request.path, strings.NewReader(request.body), cookies)
		payload := readAll(t, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", request.path, response.StatusCode, payload)
		}
	}
	legacyModel := "legacy-" + model
	now := time.Now().UTC()
	if err := app.db.InsertUsageRecord(t.Context(), db.UsageRecord{Finalized: true, OrganizationUUID: provider.OrganizationUUID, WorkspaceUUID: provider.WorkspaceUUID, AttemptID: uuid.NewV4().String(), Source: "api", Model: legacyModel, StartedAt: now, EndedAt: now, ModelCents: new(int64(0)), Complete: true, Version: "request_cents_v1"}); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/usage_cost", "/usage_activities"} {
		response := app.platformRequest(t, http.MethodGet, prefix+endpoint+"?workspace_id="+provider.WorkspaceUUID, nil, cookies)
		var report struct {
			Options struct {
				Models []string `json:"models"`
			} `json:"options"`
		}
		err := json.UnmarshalRead(response.Body, &report)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("model options: %d %v", response.StatusCode, err)
		}
		if slices.Contains(report.Options.Models, model) || !slices.Contains(report.Options.Models, qualifiedModel) || !slices.Contains(report.Options.Models, legacyModel) {
			t.Fatalf("model options must use qualified provider IDs and preserve ledger IDs: %v", report.Options.Models)
		}
	}
	for _, tc := range []struct {
		filter   string
		requests int64
	}{
		{"", 5}, {"&source=api", 2}, {"&source=workbench", 3}, {"&source=managed_agents", 0},
	} {
		// HTTP 响应可能先于上游 Body.Close 的落账结束，等待账本完成后再核对金额。
		var report analytics.Report
		deadline := time.Now().Add(3 * time.Second)
		for {
			response := app.platformRequest(t, http.MethodGet, prefix+"/usage_cost?model="+qualifiedModel+tc.filter, nil, cookies)
			err := json.UnmarshalRead(response.Body, &report)
			response.Body.Close()
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("report: %d %v", response.StatusCode, err)
			}
			if report.Summary.Requests == tc.requests || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		want := tc.requests
		if report.Summary.Requests != want || report.Summary.ModelCents != 4*want || report.Summary.SearchCents != 2*want || report.Summary.SubtotalCents != 6*want || report.Summary.CacheReadTokens != 1000*want || report.Summary.CacheWriteTokens != 1000*want || report.Summary.Incomplete != 0 {
			t.Fatalf("filter=%s summary=%+v", tc.filter, report.Summary)
		}
	}
	for _, suffix := range []string{"?starting_at=bad", "?timezone=invalid", "?api_key_id=invalid"} {
		response := app.platformRequest(t, http.MethodGet, prefix+"/usage_cost"+suffix, nil, cookies)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatal(fmt.Sprintf("invalid query %s: %d", suffix, response.StatusCode))
		}
	}
}

func TestAnalyticsWorkspaceIsolation(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("analytics-isolation"))
	defer app.close()
	refs := getAdminDefaultIDs(t, app.pool)
	private := createAdminWorkspace(t, app, "分析私有空间-"+uniqueAdminSuffix(), nil, nil)
	workspace, err := app.db.GetAdminWorkspace(t.Context(), refs.OrganizationUUID, private.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	model := "private-analytics-model"
	err = app.db.InsertUsageRecord(t.Context(), db.UsageRecord{Finalized: true, OrganizationUUID: refs.OrganizationUUID, WorkspaceUUID: workspace.UUID, AttemptID: uuid.NewV4().String(), Source: "api", Model: model, StartedAt: now, EndedAt: now, ModelCents: new(int64(123)), Complete: true, Version: "request_cents_v1"})
	if err != nil {
		t.Fatal(err)
	}
	user := seedAdminUser(t, app.pool, "analytics-"+uniqueAdminSuffix()+"@example.local", "user")
	cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, user)
	prefix := "/api/organizations/" + refs.OrganizationUUID
	check := func(want int64) {
		t.Helper()
		response := app.platformRequest(t, http.MethodGet, prefix+"/usage_cost?model="+model, nil, cookies)
		var result struct {
			analytics.Report
			Options struct {
				Models     []string `json:"models"`
				Workspaces []struct {
					Value string `json:"value"`
				} `json:"workspaces"`
			} `json:"options"`
		}
		err := json.UnmarshalRead(response.Body, &result)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || result.Summary.ModelCents != want {
			t.Fatalf("report=%+v status=%d err=%v", result, response.StatusCode, err)
		}
		if want > 0 {
			found := false
			for _, option := range result.Options.Workspaces {
				if option.Value == private.ID {
					found = true
				}
			}
			if !found {
				t.Fatal("工作区选项未使用控制台 ID")
			}
		}
		if want == 0 {
			for _, option := range result.Options.Models {
				if option == model {
					t.Fatal("私有模型选项泄漏")
				}
			}
		}
	}
	check(0)
	response := app.platformRequest(t, http.MethodGet, prefix+"/usage_cost?workspace_id="+private.ID, nil, cookies)
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("越权状态=%d", response.StatusCode)
	}
	app.seedPlatformSession(t, "analytics-owner")
	owner, err := app.sessions.Get(t.Context(), "analytics-owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = workspaceaccess.ChangeMember(t.Context(), app.db, owner.Principal(), private.ID, user, "workspace_user", "create"); err != nil {
		t.Fatal(err)
	}
	check(123)
	if _, err = workspaceaccess.ChangeMember(t.Context(), app.db, owner.Principal(), private.ID, user, "", "delete"); err != nil {
		t.Fatal(err)
	}
	check(0)
	response = app.platformRequest(t, http.MethodGet, prefix+"/workspaces/"+private.ID+"/usage_cost?workspace_id=default", nil, cookies)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("冲突状态=%d", response.StatusCode)
	}
}

//go:build integration || e2e || e2b_integration

package liveworker

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"net/http"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const isolatedProviderName = "isolated worker model metadata"

func ensureIsolatedWorkerProvider(t *testing.T, e *liveEnv, cfg config.Config) {
	t.Helper()
	encoded, err := jsonv2.Marshal(map[string]any{"credentials": map[string]string{"method": "code", "code": "123456", "email_address": cfg.Bootstrap.UserEmail}})
	requireOK(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url+"/api/auth/verify_magic_link", bytes.NewReader(encoded))
	requireOK(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	requireOK(t, err)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("isolated provider login: status=%d", response.StatusCode)
	}
	e.cookies = response.Cookies()
	providers := isolatedWorkerProviders(t, e)
	if len(providers) != 0 {
		return
	}
	var created struct {
		ID string `json:"id"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, http.MethodPost, isolatedProviderPath(e), "", isolatedProviderInput("http://127.0.0.1:9"), http.StatusCreated, e.cookies...), &created))
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, http.MethodDelete, isolatedProviderPath(e)+"/"+created.ID, "", nil, http.StatusNoContent, e.cookies...)
	})
}

func configureIsolatedChatModel(t *testing.T, e *liveEnv, modelURL string) {
	t.Helper()
	providers := isolatedWorkerProviders(t, e)
	if len(providers) != 1 || providers[0].Name != isolatedProviderName {
		t.Fatal("chat verification requires a fresh isolated workspace with its own fixture provider")
	}
	e.request(t, http.MethodPut, isolatedProviderPath(e)+"/"+providers[0].ID, "", isolatedProviderInput(modelURL), http.StatusOK, e.cookies...)
}

type isolatedWorkerProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func isolatedWorkerProviders(t *testing.T, e *liveEnv) []isolatedWorkerProvider {
	t.Helper()
	var providers []isolatedWorkerProvider
	requireOK(t, jsonv2.Unmarshal(e.request(t, http.MethodGet, isolatedProviderPath(e), "", nil, http.StatusOK, e.cookies...), &providers))
	return providers
}

func isolatedProviderPath(e *liveEnv) string {
	return "/api/console/organizations/" + e.key.OrganizationUUID.String() + "/workspaces/default/llm_providers"
}

func isolatedProviderInput(baseURL string) map[string]any {
	return map[string]any{
		"name": isolatedProviderName, "slug": "default", "api_backend": "messages",
		"base_url": baseURL, "api_key": "isolated-fake",
		"model_ids": []string{"claude-opus-4-6", "claude-sonnet-4-6"},
	}
}

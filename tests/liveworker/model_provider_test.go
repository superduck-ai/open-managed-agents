//go:build integration || e2e || e2b_integration

package liveworker

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

var isolatedProviderFixtures sync.Map

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
	ownerKey := e.url + isolatedProviderPath(e)
	if len(providers) != 0 {
		owned, ok := isolatedProviderFixtures.Load(ownerKey)
		if ok && len(providers) == 1 && providers[0].ID == owned && providers[0].Name == isolatedProviderName {
			return
		}
		t.Fatal("worker verification requires an empty isolated workspace before creating its fixture provider")
	}
	var created struct {
		ID string `json:"id"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, http.MethodPost, isolatedProviderPath(e), "", isolatedProviderInput("http://127.0.0.1:9"), http.StatusCreated, e.cookies...), &created))
	isolatedProviderFixtures.Store(ownerKey, created.ID)
	t.Cleanup(func() {
		defer isolatedProviderFixtures.Delete(ownerKey)
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

func TestIsolatedWorkerProviderRejectsExistingCatalog(t *testing.T) {
	if name := os.Getenv("OMA_TEST_EXISTING_PROVIDER"); name != "" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/auth/verify_magic_link" {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			if r.Method != http.MethodGet {
				t.Errorf("modified existing provider with %s", r.Method)
			}
			encoded, err := jsonv2.Marshal([]isolatedWorkerProvider{{ID: "llmprov_existing", Name: name}})
			requireOK(t, err)
			_, _ = w.Write(encoded)
		}))
		defer server.Close()
		ensureIsolatedWorkerProvider(t, &liveEnv{url: server.URL}, config.Config{})
		t.Fatal("existing catalog was silently accepted")
	}
	for _, name := range []string{"unrelated provider", isolatedProviderName} {
		t.Run(name, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestIsolatedWorkerProviderRejectsExistingCatalog$")
			command.Env = append(os.Environ(), "OMA_TEST_EXISTING_PROVIDER="+name)
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "requires an empty isolated workspace") {
				t.Fatalf("fixture did not reject the existing catalog at setup: %v\n%s", err, output)
			}
			if strings.Contains(string(output), "modified existing provider") {
				t.Fatal(string(output))
			}
		})
	}
}

func TestIsolatedWorkerProviderCreatesAndCleansOwnCatalog(t *testing.T) {
	var methods []string
	var created bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth/verify_magic_link" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		methods = append(methods, r.Method)
		switch r.Method {
		case http.MethodGet:
			if created {
				_, _ = w.Write([]byte(`[{"id":"llmprov_created","name":"isolated worker model metadata"}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case http.MethodPost:
			created = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"llmprov_created"}`))
		case http.MethodDelete:
			if !strings.HasSuffix(r.URL.Path, "/llmprov_created") {
				t.Errorf("deleted unexpected provider: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s", r.Method)
		}
	}))
	defer server.Close()
	t.Run("fixture", func(t *testing.T) {
		ensureIsolatedWorkerProvider(t, &liveEnv{url: server.URL}, config.Config{})
		ensureIsolatedWorkerProvider(t, &liveEnv{url: server.URL}, config.Config{})
	})
	if strings.Join(methods, ",") != "GET,POST,GET,DELETE" {
		t.Fatalf("fixture requests: %v", methods)
	}
}

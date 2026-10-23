//go:build integration

package tests

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestLLMProviderContextWindowsPersist(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("context-window-test"))
	defer app.close()
	clearTestLLMProviders(t, app)
	org := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "model-context@example.com")
	for _, scope := range []string{"workspace", "organization"} {
		t.Run(scope, func(t *testing.T) {
			path := "/api/console/organizations/" + org + "/llm_provider"
			method := http.MethodPut
			wantStatus := http.StatusCreated
			if scope == "workspace" {
				path = "/api/console/organizations/" + org + "/workspaces/default/llm_providers"
				method = http.MethodPost
				wantStatus = http.StatusCreated
			}
			response := app.platformRequest(t, method, path, strings.NewReader(`{"name":"Context test","base_url":"https://models.example.com","api_key":"test-key","model_ids":["test-model"],"model_context_windows":{"test-model":1000000},"model_options":{"test-model":{"max_output_tokens":128000,"input_modalities":["text","image"]}}}`), cookies)
			var created struct {
				Options map[string]db.ModelOptions `json:"model_options"`
				ID      string                     `json:"id"`
				Windows map[string]int             `json:"model_context_windows"`
			}
			if response.StatusCode != wantStatus {
				t.Fatalf("create: %d %s", response.StatusCode, readAll(t, response.Body))
			}
			decodeJSON(t, response.Body, &created)
			response.Body.Close()
			if created.Options["test-model"].MaxOutputTokens == nil || *created.Options["test-model"].MaxOutputTokens != 128000 || !reflect.DeepEqual(created.Options["test-model"].InputModalities, []string{"text", "image"}) {
				t.Fatalf("created options: %+v", created.Options)
			}
			if created.Windows["test-model"] != 1000000 {
				t.Fatalf("created windows: %v", created.Windows)
			}
			if scope == "workspace" {
				path += "/" + created.ID
			}
			for _, windows := range []string{"", `,"model_context_windows":{}`, `,"model_context_windows":{"test-model":256000}`} {
				update := app.platformRequest(t, http.MethodPut, path, strings.NewReader(fmt.Sprintf(`{"name":"Context test","base_url":"https://models.example.com","model_ids":["test-model"]%s}`, windows)), cookies)
				if update.StatusCode != http.StatusOK {
					t.Fatalf("update: %d %s", update.StatusCode, readAll(t, update.Body))
				}
				var result struct {
					Options map[string]db.ModelOptions `json:"model_options"`
					Windows map[string]int             `json:"model_context_windows"`
				}
				decodeJSON(t, update.Body, &result)
				update.Body.Close()
				if !reflect.DeepEqual(result.Options, created.Options) {
					t.Fatalf("omitted options lost: %+v", result.Options)
				}
				want := 1000000
				if strings.Contains(windows, "{}") {
					want = 0
				} else if windows != "" {
					want = 256000
				}
				if result.Windows["test-model"] != want {
					t.Fatalf("saved window=%v want=%d", result.Windows, want)
				}
			}
			for _, options := range []string{`{"test-model":{"max_output_tokens":8000,"input_modalities":["image"]}}`, `{}`} {
				update := app.platformRequest(t, http.MethodPut, path, strings.NewReader(`{"name":"Context test","base_url":"https://models.example.com","model_ids":["test-model"],"model_options":`+options+`}`), cookies)
				if update.StatusCode != http.StatusOK {
					t.Fatalf("options update: %d %s", update.StatusCode, readAll(t, update.Body))
				}
				update.Body.Close()
				readPath := path
				if scope == "workspace" {
					readPath = strings.TrimSuffix(path, "/"+created.ID)
				}
				saved := app.platformRequest(t, http.MethodGet, readPath, nil, cookies)
				if saved.StatusCode != http.StatusOK {
					t.Fatalf("read saved: %d", saved.StatusCode)
				}
				var got struct {
					Options map[string]db.ModelOptions `json:"model_options"`
				}
				if scope == "organization" {
					decodeJSON(t, saved.Body, &got)
				} else {
					var items []struct {
						Options map[string]db.ModelOptions `json:"model_options"`
					}
					decodeJSON(t, saved.Body, &items)
					if len(items) != 1 {
						t.Fatalf("providers: %d", len(items))
					}
					got.Options = items[0].Options
				}
				if options == "{}" {
					if len(got.Options) != 0 {
						t.Fatal("options not cleared")
					}
				} else if got.Options["test-model"].MaxOutputTokens == nil || *got.Options["test-model"].MaxOutputTokens != 8000 || !reflect.DeepEqual(got.Options["test-model"].InputModalities, []string{"image"}) {
					t.Fatalf("options not persisted: %+v", got.Options)
				}

				saved.Body.Close()
			}
		})
	}
}

//go:build integration

package tests

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestLLMProviderModelsPersistAndLegacyIDsPreserveParameters(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("provider-models-test"))
	clearTestLLMProviders(t, app)
	org := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "model-config@example.com")
	for _, scope := range []string{"workspace", "organization"} {
		t.Run(scope, func(t *testing.T) {
			path := "/api/console/organizations/" + org + "/llm_provider"
			method := http.MethodPut
			if scope == "workspace" {
				path = "/api/console/organizations/" + org + "/workspaces/default/llm_providers"
				method = http.MethodPost
			}
			response := app.platformRequest(t, method, path, strings.NewReader(`{"name":"Models test","base_url":"https://models.example.com","api_key":"test-key","models":[{"id":"configured","context_window":1000000,"max_output_tokens":128000,"input_modalities":["text","image"]},{"id":"default"}]}`), cookies)
			if response.StatusCode != http.StatusCreated {
				t.Fatalf("create: %d %s", response.StatusCode, readAll(t, response.Body))
			}
			var created struct {
				ID       string       `json:"id"`
				Models   db.LLMModels `json:"models"`
				ModelIDs []string     `json:"model_ids"`
			}
			decodeJSON(t, response.Body, &created)
			response.Body.Close()
			if !reflect.DeepEqual(created.ModelIDs, []string{"configured", "default"}) {
				t.Fatalf("legacy projection: %v", created.ModelIDs)
			}
			if scope == "workspace" {
				path += "/" + created.ID
			}
			load := func() db.LLMProvider {
				t.Helper()
				var provider db.LLMProvider
				var err error
				if scope == "workspace" {
					provider, err = app.db.GetLLMProvider(t.Context(), org, getDefaultDBIDs(t, app.pool).WorkspaceUUID, created.ID)
				} else {
					provider, err = app.db.GetOrganizationLLMProvider(t.Context(), org)
				}
				if err != nil {
					t.Fatal(err)
				}
				return provider
			}
			if !reflect.DeepEqual(load().Models, created.Models) {
				t.Fatal("model settings were not persisted")
			}
			for _, test := range []struct {
				payload string
				want    db.LLMModels
			}{
				{`"model_ids":["configured","new"]`, db.LLMModels{created.Models[0], {ID: "new"}}},
				{`"models":[{"id":"configured"}]`, db.LLMModels{{ID: "configured"}}},
				{`"models":[]`, db.LLMModels{}},
			} {
				update := app.platformRequest(t, http.MethodPut, path, strings.NewReader(`{"name":"Models test","base_url":"https://models.example.com",`+test.payload+`}`), cookies)
				if update.StatusCode != http.StatusOK {
					t.Fatalf("update: %d %s", update.StatusCode, readAll(t, update.Body))
				}
				update.Body.Close()
				if got := load().Models; !reflect.DeepEqual(got, test.want) {
					t.Fatalf("%s: got %+v want %+v", test.payload, got, test.want)
				}
			}
		})
	}
}

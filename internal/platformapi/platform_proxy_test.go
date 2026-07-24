package platformapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/go-chi/chi/v5"
)

func TestRewriteMappedModelOnlyUpdatesTopLevelModel(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-6",
		"tools":[{
			"name":"build_agent_config",
			"input_schema":{"properties":{"model":{"anyOf":[
				{"type":"string","enum":["claude-sonnet-4-6"]}
			]}}}
		}]
	}`)
	got, err := rewriteMappedModel(body, map[string]string{
		"claude-sonnet-4-6": "glm-5-turbo",
	})
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Model string            `json:"model"`
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(got, &request); err != nil {
		t.Fatalf("decode rewritten request: %v", err)
	}
	if request.Model != "glm-5-turbo" {
		t.Fatalf("model = %q, want glm-5-turbo", request.Model)
	}
	var schema struct {
		InputSchema json.RawMessage `json:"input_schema"`
	}
	if err := json.Unmarshal(request.Tools[0], &schema); err != nil {
		t.Fatalf("decode preserved tool: %v", err)
	}
	if !bytes.Contains(schema.InputSchema, []byte(`claude-sonnet-4-6`)) {
		t.Fatalf("tool schema was unexpectedly rewritten: %s", schema.InputSchema)
	}
}

func TestRewriteMappedModelTrimsRequestModelID(t *testing.T) {
	got, err := rewriteMappedModel(
		[]byte(`{"model":" claude-sonnet-4-6 ","max_tokens":16}`),
		map[string]string{"claude-sonnet-4-6": "glm-5-turbo"},
	)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(got, &request); err != nil {
		t.Fatal(err)
	}
	if request.Model != "glm-5-turbo" {
		t.Fatalf("model = %q, want glm-5-turbo", request.Model)
	}
}

func TestRewriteMappedModelPreservesInvalidJSONForUpstream(t *testing.T) {
	body := []byte(`not-json`)

	got, err := rewriteMappedModel(body, map[string]string{
		"claude-sonnet-4-6": "glm-5-turbo",
	})
	if err != nil {
		t.Fatalf("rewriteMappedModel() error = %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("rewriteMappedModel() = %q, want original body %q", got, body)
	}
}

func TestOrganizationProxyModelMappings(t *testing.T) {
	testCases := []struct {
		name           string
		mappings       map[string]string
		requestedModel string
		wantModel      string
	}{
		{
			name: "mapped model",
			mappings: map[string]string{
				"claude-sonnet-4-6": "glm-5-turbo",
			},
			requestedModel: "claude-sonnet-4-6",
			wantModel:      "glm-5-turbo",
		},
		{
			name: "unmapped model",
			mappings: map[string]string{
				"claude-opus-4-8": "glm-5.2",
			},
			requestedModel: "claude-sonnet-4-6",
			wantModel:      "claude-sonnet-4-6",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var upstreamBody []byte
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var err error
				upstreamBody, err = io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read upstream body: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"msg_model_mapping","type":"message"}`))
			}))
			defer upstream.Close()

			cfg := config.Config{
				AnthropicUpstream: config.AnthropicUpstreamConfig{
					BaseURL:       upstream.URL,
					APIKey:        "upstream-key",
					ModelMappings: testCase.mappings,
				},
			}
			requestBody := `{"model":"` + testCase.requestedModel + `","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/organizations/org_test/proxy/v1/messages",
				strings.NewReader(requestBody),
			)
			routeContext := chi.NewRouteContext()
			routeContext.URLParams.Add("orgUuid", "org_test")
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeContext)
			ctx = auth.WithPrincipal(ctx, auth.Principal{OrganizationUUID: "org_test"})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handleProxyMessages(cfg).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("proxy status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			var payload struct {
				Model     string `json:"model"`
				MaxTokens int    `json:"max_tokens"`
				Messages  []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(upstreamBody, &payload); err != nil {
				t.Fatalf("decode upstream body: %v", err)
			}
			if payload.Model != testCase.wantModel {
				t.Fatalf("upstream model = %v, want %s", payload.Model, testCase.wantModel)
			}
			if payload.MaxTokens != 16 || len(payload.Messages) != 1 {
				t.Fatalf("upstream body lost request fields: %#v", payload)
			}
		})
	}
}

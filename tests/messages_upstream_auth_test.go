package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestMessagesUpstreamAuthenticationFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"anthropic", `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`},
		{"generic", `{"error":{"message":"invalid key","code":"invalid_api_key"}}`},
		{"non_json", "provider rejected credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Request-Id", "provider-auth-error")
				w.Header().Set("Retry-After", "60")
				w.Header().Set("WWW-Authenticate", "Bearer")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer upstream.Close()
			app := newPayloadIntegrationApp(t, newFakeStore("upstream-auth-error"))
			clearTestLLMProviders(t, app)
			seedTestLLMProvider(t, app, "Authentication failure", upstream.URL, "provider-key", messagesTestModel)
			agent := createAgent(t, app, `{"model":"`+messagesTestModel+`","name":"upstream-authentication"}`)
			environment := createEnvironment(t, app, `{"name":"upstream-authentication"}`)
			createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
			credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
			registerCodeSessionWorker(t, app, credential.CodeSessionID)
			code, found, err := app.db.GetCodeSession(t.Context(), credential.CodeSessionID)
			if err != nil || !found {
				t.Fatalf("Code Session found=%v error=%v", found, err)
			}
			response := doMessagesRequest(t, app, credential.Token, `{"model":"`+messagesTestModel+`","messages":[]}`)
			requestID := response.Header.Get("Request-Id")
			if response.Header.Get("X-Should-Retry") != "false" || response.Header.Get("Retry-After") != "" || response.Header.Get("WWW-Authenticate") != "" {
				t.Fatalf("unexpected retry or authentication headers: %#v", response.Header)
			}
			assertError(t, response, http.StatusForbidden, "permission_error")
			events := requestLifecycleEvents(t, app, code, 2)
			var end struct {
				StartID           string `json:"model_request_start_id"`
				UpstreamRequestID string `json:"upstream_request_id"`
				IsError           bool   `json:"is_error"`
				Error             struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(events[1].Payload, &end); err != nil {
				t.Fatal(err)
			}
			if events[0].EventType != "span.model_request_start" || events[1].EventType != "span.model_request_end" || requestID != events[0].ExternalID || end.StartID != requestID || !end.IsError || end.Error.Type != "http_error" || end.UpstreamRequestID != "provider-auth-error" {
				t.Fatalf("request ID=%s events=%+v end=%+v", requestID, events, end)
			}
			response = doMessagesRequest(t, app, defaultTestKey, `{"model":"`+messagesTestModel+`","messages":[]}`)
			defer response.Body.Close()
			body := readAll(t, response.Body)
			if response.StatusCode != http.StatusUnauthorized || string(body) != tc.body || response.Header.Get("Retry-After") != "60" || !slices.Contains(response.Header.Values("Request-Id"), "provider-auth-error") {
				t.Fatalf("API-key proxy response: status=%d headers=%#v body=%s", response.StatusCode, response.Header, body)
			}
		})
	}
}

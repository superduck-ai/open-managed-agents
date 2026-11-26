package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestEventIDAndSessionResourceCodecRoundTrip(t *testing.T) {
	if got := EncodeEventID("evt_abc123"); got != "sevt_qdr_abc123" {
		t.Fatalf("EncodeEventID = %q, want sevt_qdr_abc123", got)
	}
	if got := EncodeEventID("sevt_qdr_abc123"); got != "sevt_qdr_abc123" {
		t.Fatalf("EncodeEventID idempotent = %q", got)
	}
	if got := DecodeEventID("sevt_qdr_abc123"); got != "evt_abc123" {
		t.Fatalf("DecodeEventID = %q, want evt_abc123", got)
	}
	if got := EncodeSessionResourceID("sesr_res42"); got != "sesrsc_qdr_res42" {
		t.Fatalf("EncodeSessionResourceID = %q, want sesrsc_qdr_res42", got)
	}
	if got := DecodeSessionResourceID("sesrsc_qdr_res42"); got != "sesr_res42" {
		t.Fatalf("DecodeSessionResourceID = %q, want sesr_res42", got)
	}

	cases := []struct {
		name     string
		remote   string
		expected string
	}{
		{
			name:     "top_level_event_id",
			remote:   `{"id":"evt_1","type":"agent.message","content":[{"type":"text","text":"hi"}]}`,
			expected: `{"id":"sevt_qdr_1","type":"agent.message","content":[{"type":"text","text":"hi"}]}`,
		},
		{
			name:     "event_start_nested_event_id",
			remote:   `{"type":"event_start","event":{"id":"evt_2","type":"agent.message"}}`,
			expected: `{"type":"event_start","event":{"id":"sevt_qdr_2","type":"agent.message"}}`,
		},
		{
			name:     "event_delta_event_id",
			remote:   `{"type":"event_delta","event_id":"evt_3","delta":{"type":"content_delta"}}`,
			expected: `{"type":"event_delta","event_id":"sevt_qdr_3","delta":{"type":"content_delta"}}`,
		},
		{
			name:     "status_idle_stop_reason_event_ids",
			remote:   `{"id":"evt_4","type":"session.status_idle","stop_reason":{"type":"requires_action","event_ids":["evt_4a","evt_4b"]}}`,
			expected: `{"id":"sevt_qdr_4","type":"session.status_idle","stop_reason":{"type":"requires_action","event_ids":["sevt_qdr_4a","sevt_qdr_4b"]}}`,
		},
		{
			name:     "requires_action_event_ids",
			remote:   `{"id":"evt_5","type":"session.status_idle","requires_action":{"event_ids":["evt_5a","evt_5b"]}}`,
			expected: `{"id":"sevt_qdr_5","type":"session.status_idle","requires_action":{"event_ids":["sevt_qdr_5a","sevt_qdr_5b"]}}`,
		},
		{
			name:     "agent_tool_result_tool_use_id",
			remote:   `{"id":"evt_6","type":"agent.tool_result","tool_use_id":"evt_6u","input":{"id":"evt_user_literal"}}`,
			expected: `{"id":"sevt_qdr_6","type":"agent.tool_result","tool_use_id":"sevt_qdr_6u","input":{"id":"evt_user_literal"}}`,
		},
		{
			name:     "agent_mcp_tool_result_mcp_tool_use_id",
			remote:   `{"id":"evt_7","type":"agent.mcp_tool_result","mcp_tool_use_id":"evt_7u"}`,
			expected: `{"id":"sevt_qdr_7","type":"agent.mcp_tool_result","mcp_tool_use_id":"sevt_qdr_7u"}`,
		},
		{
			name:     "user_tool_confirmation_tool_use_id",
			remote:   `{"id":"evt_8","type":"user.tool_confirmation","tool_use_id":"evt_8u","result":"allow"}`,
			expected: `{"id":"sevt_qdr_8","type":"user.tool_confirmation","tool_use_id":"sevt_qdr_8u","result":"allow"}`,
		},
		{
			name:     "user_tool_result_tool_use_id",
			remote:   `{"id":"evt_9","type":"user.tool_result","tool_use_id":"evt_9u"}`,
			expected: `{"id":"sevt_qdr_9","type":"user.tool_result","tool_use_id":"sevt_qdr_9u"}`,
		},
		{
			name:     "user_custom_tool_result_custom_tool_use_id",
			remote:   `{"id":"evt_10","type":"user.custom_tool_result","custom_tool_use_id":"evt_10u"}`,
			expected: `{"id":"sevt_qdr_10","type":"user.custom_tool_result","custom_tool_use_id":"sevt_qdr_10u"}`,
		},
		{
			name:     "span_model_request_end_start_id",
			remote:   `{"id":"evt_11","type":"span.model_request_end","model_request_start_id":"evt_11s"}`,
			expected: `{"id":"sevt_qdr_11","type":"span.model_request_end","model_request_start_id":"sevt_qdr_11s"}`,
		},
		{
			name:     "pagination_cursors",
			remote:   `{"before_id":"evt_12a","after_id":"evt_12b","first_id":"evt_12c","last_id":"evt_12d"}`,
			expected: `{"before_id":"sevt_qdr_12a","after_id":"sevt_qdr_12b","first_id":"sevt_qdr_12c","last_id":"sevt_qdr_12d"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := EncodeEventPayload(json.RawMessage(tc.remote))
			if err != nil {
				t.Fatalf("EncodeEventPayload: %v", err)
			}
			assertJSONEqual(t, encoded, json.RawMessage(tc.expected))

			decoded, err := DecodeEventPayload(encoded)
			if err != nil {
				t.Fatalf("DecodeEventPayload: %v", err)
			}
			assertJSONEqual(t, decoded, json.RawMessage(tc.remote))
		})
	}
}

func TestTranslateToolsBidirectional(t *testing.T) {
	input := json.RawMessage(`[
		{
			"type": "agent_toolset_20260401",
			"default_config": {
				"enabled": true,
				"permission_policy": {"type": "always_allow"}
			},
			"configs": [
				{"name": "bash", "type": "bash", "enabled": true, "permission_policy": {"type": "always_ask"}},
				{"name": "web_fetch", "enabled": true, "permission_policy": {"type": "always_deny"}},
				{"name": "ask_user_question", "type": "ask_user_question", "enabled": true},
				{"name": "get_port_url", "enabled": true}
			]
		},
		{
			"type": "custom",
			"name": "lookup_order"
		}
	]`)

	toQoder, err := TranslateToolsToQoder(input)
	if err != nil {
		t.Fatalf("TranslateToolsToQoder: %v", err)
	}
	var qoderTools []map[string]json.RawMessage
	if err := json.Unmarshal(toQoder, &qoderTools); err != nil {
		t.Fatalf("unmarshal qoder tools: %v", err)
	}
	if len(qoderTools) != 2 {
		t.Fatalf("len(qoderTools) = %d, want 2", len(qoderTools))
	}
	var configs []struct {
		Name             string          `json:"name"`
		Enabled          *bool           `json:"enabled"`
		PermissionPolicy json.RawMessage `json:"permission_policy"`
	}
	if err := json.Unmarshal(qoderTools[0]["configs"], &configs); err != nil {
		t.Fatalf("unmarshal configs: %v", err)
	}
	names := make([]string, 0, len(configs))
	for _, c := range configs {
		names = append(names, c.Name)
	}
	if !slices.Equal(names, []string{"Bash", "WebFetch", "DeliverArtifacts"}) {
		t.Fatalf("translated config names = %v, want [Bash WebFetch DeliverArtifacts]", names)
	}
	if string(configs[0].PermissionPolicy) != `{"type":"always_ask"}` {
		t.Fatalf("bash permission_policy = %s", configs[0].PermissionPolicy)
	}

	backFromQoder, err := TranslateToolsFromQoder(toQoder)
	if err != nil {
		t.Fatalf("TranslateToolsFromQoder: %v", err)
	}
	var roundTripped []map[string]json.RawMessage
	if err := json.Unmarshal(backFromQoder, &roundTripped); err != nil {
		t.Fatalf("unmarshal backFromQoder: %v", err)
	}
	var backConfigs []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(roundTripped[0]["configs"], &backConfigs); err != nil {
		t.Fatalf("unmarshal backConfigs: %v", err)
	}
	backNames := make([]string, 0, len(backConfigs))
	for _, c := range backConfigs {
		backNames = append(backNames, c.Name)
	}
	if !slices.Equal(backNames, []string{"bash", "web_fetch", "deliver_artifacts"}) {
		t.Fatalf("reverse config names = %v, want [bash web_fetch deliver_artifacts]", backNames)
	}

	enabledInput := json.RawMessage(`[{"type":"agent_toolset_20260401","enabled_tools":["read","web_search","ask_user_question"]}]`)
	qoderEnabledRaw, err := TranslateToolsToQoder(enabledInput)
	if err != nil {
		t.Fatalf("TranslateToolsToQoder(enabled_tools): %v", err)
	}
	var qoderEnabledTools []struct {
		EnabledTools []string `json:"enabled_tools"`
	}
	if err := json.Unmarshal(qoderEnabledRaw, &qoderEnabledTools); err != nil {
		t.Fatalf("unmarshal qoderEnabledRaw: %v", err)
	}
	if !slices.Equal(qoderEnabledTools[0].EnabledTools, []string{"Read", "WebSearch", "DeliverArtifacts"}) {
		t.Fatalf("qoder enabled_tools = %v", qoderEnabledTools[0].EnabledTools)
	}

	fromEnabledRaw, err := TranslateToolsFromQoder(qoderEnabledRaw)
	if err != nil {
		t.Fatalf("TranslateToolsFromQoder(enabled_tools): %v", err)
	}
	var fromEnabled []struct {
		EnabledTools []string `json:"enabled_tools"`
		Configs      []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"configs"`
	}
	if err := json.Unmarshal(fromEnabledRaw, &fromEnabled); err != nil {
		t.Fatalf("unmarshal fromEnabledRaw: %v", err)
	}
	if !slices.Equal(fromEnabled[0].EnabledTools, []string{"read", "web_search", "deliver_artifacts"}) {
		t.Fatalf("reverse enabled_tools = %v", fromEnabled[0].EnabledTools)
	}
	if len(fromEnabled[0].Configs) != 3 || fromEnabled[0].Configs[2].Name != "deliver_artifacts" || !fromEnabled[0].Configs[2].Enabled {
		t.Fatalf("synthesized configs = %+v", fromEnabled[0].Configs)
	}

	emptyQoder, err := TranslateToolsToQoder(nil)
	if err != nil {
		t.Fatalf("TranslateToolsToQoder(nil): %v", err)
	}
	var emptyParsed []struct {
		Type    string `json:"type"`
		Configs []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"configs"`
	}
	if err := json.Unmarshal(emptyQoder, &emptyParsed); err != nil {
		t.Fatalf("unmarshal emptyQoder: %v", err)
	}
	if len(emptyParsed) != 1 || emptyParsed[0].Type != agentToolsetType || len(emptyParsed[0].Configs) != 1 || emptyParsed[0].Configs[0].Name != "DeliverArtifacts" {
		t.Fatalf("empty default toolset = %+v", emptyParsed)
	}
}

func TestFactoryCredentialsEndpointHealthAndErrorMapping(t *testing.T) {
	statusCode := http.StatusOK
	modelsBody := `{"data":[{"id":"qmodel_latest","display_name":"QModel","default_context_window":200000,"is_vl":true,"is_enabled":true},{"id":"disabled_m","display_name":"Disabled","is_enabled":false}]}`
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(modelsBody))
	}))
	defer srv.Close()

	factory := NewFactory(agentruntime.NewBindingService(newTestBindingStore()), nil, nil)

	for _, cred := range []string{`{"token":"tok-a"}`, `{"api_key":"tok-b"}`, "  tok-plain  "} {
		rt, err := factory(context.Background(), agentruntime.ProfileRuntimeConfig{
			Profile: db.RuntimeProfile{
				UUID:         "prof-1",
				ProviderKind: string(agentruntime.ProviderQoderCloud),
				EndpointURL:  srv.URL,
			},
			Credentials:  []byte(cred),
			Capabilities: agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud),
		})
		if err != nil {
			t.Fatalf("factory(%q) error = %v", cred, err)
		}
		if rt.ProviderKind() != agentruntime.ProviderQoderCloud {
			t.Fatalf("ProviderKind = %s", rt.ProviderKind())
		}
		models, err := rt.(agentruntime.ModelRuntime).ListModels(context.Background(), agentruntime.Scope{})
		if err != nil || len(models) != 1 || models[0].ID != "qmodel_latest" || !models[0].IsVL {
			t.Fatalf("ListModels = (%+v, %v)", models, err)
		}
		_ = rt.Close()
	}
	if gotAuth != "Bearer tok-plain" {
		t.Fatalf("Authorization header = %q, want Bearer tok-plain", gotAuth)
	}

	if _, err := factory(context.Background(), agentruntime.ProfileRuntimeConfig{
		Profile:     db.RuntimeProfile{UUID: "prof-1", ProviderKind: string(agentruntime.ProviderQoderCloud)},
		Credentials: []byte("   "),
	}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("empty credentials err = %v, want ErrInvalidCredentials", err)
	}

	defaultRT, err := factory(context.Background(), agentruntime.ProfileRuntimeConfig{
		Profile:     db.RuntimeProfile{UUID: "prof-default", ProviderKind: string(agentruntime.ProviderQoderCloud)},
		Credentials: []byte("pat-default"),
	})
	if err != nil {
		t.Fatalf("default endpoint factory err = %v", err)
	}
	if defaultRT.(*Runtime).endpoint != DefaultEndpoint {
		t.Fatalf("default endpoint = %q, want %q", defaultRT.(*Runtime).endpoint, DefaultEndpoint)
	}
	_ = defaultRT.Close()

	rt, err := factory(context.Background(), agentruntime.ProfileRuntimeConfig{
		Profile: db.RuntimeProfile{
			UUID:         "prof-health",
			ProviderKind: string(agentruntime.ProviderQoderCloud),
			EndpointURL:  srv.URL,
		},
		Credentials: []byte(`{"token":"tok-health"}`),
	})
	if err != nil {
		t.Fatalf("factory health err = %v", err)
	}
	defer rt.Close()

	hc := rt.(agentruntime.HealthChecker)
	res, err := hc.HealthCheck(context.Background())
	if err != nil || res.Status != "healthy" {
		t.Fatalf("HealthCheck(ok) = (%+v, %v)", res, err)
	}

	modelsBody = `{"data":[{"id":"off","is_enabled":false}]}`
	res, err = hc.HealthCheck(context.Background())
	if err == nil || res.Status != "unhealthy" {
		t.Fatalf("HealthCheck(no enabled models) = (%+v, %v)", res, err)
	}

	errorCases := []struct {
		status   int
		body     string
		wantKind apperr.Kind
		wantErr  error
	}{
		{http.StatusUnauthorized, `{"error":{"type":"authentication_error","message":"bad token"}}`, apperr.Unauthenticated, ErrUpstreamAuth},
		{http.StatusPaymentRequired, `{"error":{"type":"billing_error","message":"insufficient credits"}}`, apperr.Billing, ErrUpstreamBilling},
		{http.StatusNotFound, `{"error":{"type":"not_found_error","message":"missing"}}`, apperr.NotFound, ErrUpstreamNotFound},
		{http.StatusConflict, `{"error":{"type":"conflict_error","message":"in use"}}`, apperr.Conflict, ErrUpstreamConflict},
		{http.StatusTooManyRequests, `{"error":{"type":"rate_limit_error","message":"slow down"}}`, apperr.RateLimited, ErrUpstreamRateLimited},
		{http.StatusServiceUnavailable, `{"error":{"type":"api_error","message":"down"}}`, apperr.Unavailable, ErrUpstreamUnavailable},
	}
	for _, tc := range errorCases {
		statusCode = tc.status
		modelsBody = tc.body
		_, err := hc.HealthCheck(context.Background())
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Kind != tc.wantKind {
			t.Fatalf("status %d mapped to %v, want apperr kind %v", tc.status, err, tc.wantKind)
		}
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("status %d err %v does not wrap %v", tc.status, err, tc.wantErr)
		}
	}
}

func assertJSONEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("unmarshal got JSON %s: %v", got, err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("unmarshal want JSON %s: %v", want, err)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", gb, wb)
	}
}

package db

import (
	"strings"
	"testing"
	"time"
)

func TestWebhookEndpointQueriesUseSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 27, 13, 30, 0, 0, time.UTC)
	commonEndpointArguments := map[string]any{
		"uuid":                    "11111111-1111-4111-8111-111111111111",
		"external_id":             "whep_test",
		"organization_uuid":       "00000000-0000-0000-0000-000000000001",
		"workspace_uuid":          "00000000-0000-0000-0000-000000000002",
		"created_by_api_key_uuid": "00000000-0000-0000-0000-000000000003",
		"url":                     "https://example.test/webhook",
		"name":                    "Test webhook",
		"description":             "Test",
		"enabled_events":          []byte(`["session.created"]`),
		"signing_secret":          "secret",
		"status":                  "enabled",
		"disabled_reason":         nil,
		"consecutive_failures":    0,
		"created_at":              now,
	}
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:         "workspace identifiers",
			query:        getWorkspaceIdentifiersQuery,
			arguments:    map[string]any{"workspace_uuid": "00000000-0000-0000-0000-000000000002"},
			wantArgCount: 1,
		},
		{
			name:         "create",
			query:        createWebhookEndpointQuery,
			arguments:    commonEndpointArguments,
			wantArgCount: 15,
		},
		{
			name:         "list",
			query:        listWebhookEndpointsQuery,
			arguments:    map[string]any{"workspace_uuid": "00000000-0000-0000-0000-000000000002"},
			wantArgCount: 1,
		},
		{
			name:  "get",
			query: getWebhookEndpointQuery,
			arguments: map[string]any{
				"workspace_uuid": "00000000-0000-0000-0000-000000000002",
				"external_id":    "whep_test",
			},
			wantArgCount: 2,
		},
		{
			name:  "update",
			query: updateWebhookEndpointQuery,
			arguments: map[string]any{
				"workspace_uuid":       "00000000-0000-0000-0000-000000000002",
				"external_id":          "whep_test",
				"url":                  "https://example.test/updated",
				"name":                 "Updated",
				"description":          "Updated",
				"enabled_events":       []byte(`["session.ended"]`),
				"status":               "enabled",
				"disabled_reason":      nil,
				"consecutive_failures": 0,
				"updated_at":           now,
			},
			wantArgCount: 10,
		},
		{
			name:  "regenerate secret",
			query: regenerateWebhookEndpointSigningSecretQuery,
			arguments: map[string]any{
				"workspace_uuid": "00000000-0000-0000-0000-000000000002",
				"external_id":    "whep_test",
				"signing_secret": "new-secret",
				"updated_at":     now,
			},
			wantArgCount: 4,
		},
		{
			name:  "delete",
			query: deleteWebhookEndpointQuery,
			arguments: map[string]any{
				"workspace_uuid": "00000000-0000-0000-0000-000000000002",
				"external_id":    "whep_test",
			},
			wantArgCount: 2,
		},
		{
			name:         "exists",
			query:        hasWebhookEndpointsQuery,
			arguments:    map[string]any{"workspace_uuid": "00000000-0000-0000-0000-000000000002"},
			wantArgCount: 1,
		},
		{
			name:  "list active for event",
			query: listActiveWebhookEndpointsForEventQuery,
			arguments: map[string]any{
				"workspace_uuid": "00000000-0000-0000-0000-000000000002",
				"event_type":     "session.created",
			},
			wantArgCount: 2,
		},
		{
			name:         "record success",
			query:        recordWebhookEndpointDeliverySuccessQuery,
			arguments:    map[string]any{"endpoint_uuid": "00000000-0000-0000-0000-000000000004"},
			wantArgCount: 1,
		},
		{
			name:  "record failure",
			query: recordWebhookEndpointDeliveryFailureQuery,
			arguments: map[string]any{
				"endpoint_uuid": "00000000-0000-0000-0000-000000000004",
				"disable_after": 20,
				"reason":        "temporary failure",
			},
			wantArgCount: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, tt.query, tt.arguments)
			if err != nil {
				t.Fatalf("bindNamed() error = %v", err)
			}
			if len(arguments) != tt.wantArgCount {
				t.Fatalf("bindNamed() arguments = %#v, want %d arguments", arguments, tt.wantArgCount)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("bound query still contains a named parameter: %s", query)
			}
		})
	}
}

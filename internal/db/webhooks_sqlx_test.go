package db

import (
	"strings"
	"testing"
	"time"
)

func TestWebhookDeliveryQueriesUseSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 27, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:  "enqueue",
			query: enqueueWebhookDeliveryJobQuery,
			arguments: map[string]any{
				"workspace_id": int64(1),
				"event_type":   "session.created",
				"event":        []byte(`{"type":"session.created"}`),
			},
			wantArgCount: 3,
		},
		{
			name:  "enqueue endpoint",
			query: enqueueWebhookDeliveryJobForEndpointQuery,
			arguments: map[string]any{
				"workspace_id":        int64(1),
				"event_type":          "session.created",
				"event":               []byte(`{"type":"session.created"}`),
				"webhook_endpoint_id": int64(2),
			},
			wantArgCount: 4,
		},
		{
			name:  "lease",
			query: leaseWebhookDeliveryJobsQuery,
			arguments: map[string]any{
				"limit":              10,
				"worker_id":          "worker_test",
				"lease_microseconds": time.Minute.Microseconds(),
			},
			wantArgCount: 3,
		},
		{
			name:         "complete",
			query:        completeWebhookDeliveryJobQuery,
			arguments:    map[string]any{"job_id": int64(3)},
			wantArgCount: 1,
		},
		{
			name:  "fail",
			query: failWebhookDeliveryJobQuery,
			arguments: map[string]any{
				"job_id":    int64(3),
				"status":    "retry",
				"run_after": now,
				"attempts":  1,
				"reason":    "temporary failure",
			},
			wantArgCount: 5,
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

package db

import (
	"reflect"
	"strings"
	"testing"
)

func TestMessageBatchRequestInsertSQLBinding(t *testing.T) {
	t.Run("rejects missing JSON parameter", func(t *testing.T) {
		_, _, err := bindNamed(postgresRebinder{}, insertMessageBatchRequestSQL, map[string]any{
			"external_id":      "msgbatchreq_test",
			"workspace_id":     int64(2),
			"message_batch_id": int64(3),
			"request_index":    4,
			"custom_id":        "custom-test",
		})
		if err == nil {
			t.Fatal("bindNamed() error = nil, want missing params error")
		}
	})

	t.Run("binds JSON cast with PostgreSQL placeholders", func(t *testing.T) {
		query, arguments, err := bindNamed(postgresRebinder{}, insertMessageBatchRequestSQL, map[string]any{
			"external_id":      "msgbatchreq_test",
			"workspace_id":     int64(2),
			"message_batch_id": int64(3),
			"request_index":    4,
			"custom_id":        "custom-test",
			"params":           `{"model":"claude-test"}`,
		})
		if err != nil {
			t.Fatalf("bindNamed() error = %v", err)
		}
		wantArguments := []any{
			"msgbatchreq_test",
			int64(2),
			int64(3),
			4,
			"custom-test",
			`{"model":"claude-test"}`,
		}
		if !reflect.DeepEqual(arguments, wantArguments) {
			t.Fatalf("bindNamed() arguments = %#v, want %#v", arguments, wantArguments)
		}
		if !strings.Contains(query, "CAST($6 AS jsonb)") {
			t.Fatalf("bindNamed() query = %q, want JSONB cast with $6", query)
		}
		if strings.Contains(query, "::") {
			t.Fatalf("bindNamed() query = %q, want no PostgreSQL shorthand casts", query)
		}
	})
}

func TestLeaseMessageBatchJobsSQLBinding(t *testing.T) {
	query, arguments, err := bindNamed(postgresRebinder{}, leaseMessageBatchJobsSQL, map[string]any{
		"limit":              5,
		"worker_id":          "worker-test",
		"lease_microseconds": int64(60_000_000),
	})
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	wantArguments := []any{5, "worker-test", int64(60_000_000)}
	if !reflect.DeepEqual(arguments, wantArguments) {
		t.Fatalf("bindNamed() arguments = %#v, want %#v", arguments, wantArguments)
	}
	if !strings.Contains(query, "limit $1") ||
		!strings.Contains(query, "locked_by = $2") ||
		!strings.Contains(query, "$3 * interval '1 microsecond'") {
		t.Fatalf("bindNamed() query = %q, want ordered PostgreSQL placeholders", query)
	}
	if strings.Contains(query, "::") {
		t.Fatalf("bindNamed() query = %q, want no PostgreSQL shorthand casts", query)
	}
}

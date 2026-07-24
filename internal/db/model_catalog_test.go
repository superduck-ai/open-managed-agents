package db

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestModelCatalogQueriesUseNamedPostgreSQLBindings(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	tests := []struct {
		name      string
		query     string
		arguments map[string]any
		wantJSON  bool
	}{
		{
			name:  "failure metadata",
			query: recordModelCatalogFailureSQL,
			arguments: map[string]any{
				"catalog_key":     modelCatalogTestKey,
				"models":          json.RawMessage(`[]`),
				"last_attempt_at": now,
				"last_error":      "upstream_timeout",
			},
			wantJSON: true,
		},
		{
			name:  "successful snapshot",
			query: saveModelCatalogSuccessSQL,
			arguments: map[string]any{
				"catalog_key":     modelCatalogTestKey,
				"models":          json.RawMessage(`[{"id":"provider/model"}]`),
				"last_attempt_at": now,
				"last_success_at": now,
			},
			wantJSON: true,
		},
		{
			name:  "snapshot lookup",
			query: getModelCatalogSnapshotSQL,
			arguments: map[string]any{
				"catalog_key": modelCatalogTestKey,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			query, values, err := bindNamed(postgresRebinder{}, test.query, test.arguments)
			if err != nil {
				t.Fatalf("bindNamed() error = %v", err)
			}
			if strings.Contains(query, ":catalog_key") || len(values) == 0 {
				t.Fatalf("bound query = %q, values = %#v", query, values)
			}
			if test.wantJSON && !strings.Contains(query, "CAST($") {
				t.Fatalf("bound query = %q, want PostgreSQL JSON cast", query)
			}
		})
	}
}

const modelCatalogTestKey = "default"

package db

import (
	"strings"
	"testing"
	"time"
)

func TestFilesystemCleanupProcessingQueriesUseSQLXNamedParameters(t *testing.T) {
	t.Run("missing argument", func(t *testing.T) {
		_, _, err := bindNamed(postgresRebinder{}, leasedFilesystemCleanupJobQuery, map[string]any{
			"job_id":   int64(17),
			"job_type": filestoreFilesystemCleanupJobType,
		})
		if err == nil {
			t.Fatal("bind named query error = nil, want missing lease_token")
		}
	})

	retiredAt := time.Date(2026, time.July, 23, 16, 0, 0, 0, time.UTC)
	arguments := map[string]any{
		"job_id":          int64(17),
		"job_type":        filestoreFilesystemCleanupJobType,
		"lease_token":     "filesystem-cleanup-worker",
		"limit":           100,
		"workspace_id":    int64(42),
		"filesystem_id":   int64(43),
		"workspace_uuid":  "00000000-0000-0000-0000-000000000042",
		"filesystem_uuid": "00000000-0000-0000-0000-000000000043",
		"entry_id":        int64(44),
		"retired_at":      retiredAt,
		"status":          "completed",
	}
	tests := []struct {
		name         string
		query        string
		wantArgCount int
	}{
		{"leased job", leasedFilesystemCleanupJobQuery, 3},
		{"workspace lock", filesystemCleanupWorkspaceLockQuery, 1},
		{"filesystem lock", filesystemCleanupFilesystemLockQuery, 1},
		{"filesystem", filesystemCleanupFilesystemQuery, 2},
		{"entries", filesystemCleanupEntriesQuery, 3},
		{"retire entry", retireFilesystemCleanupEntryQuery, 3},
		{"files remain", filesystemCleanupFilesRemainQuery, 2},
		{"retire directories", retireFilesystemCleanupDirectoriesQuery, 4},
		{"complete batch", completeFilesystemCleanupBatchQuery, 6},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, boundArguments, err := bindNamed(postgresRebinder{}, test.query, arguments)
			if err != nil {
				t.Fatalf("bind named query: %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("query retains colon syntax after binding: %q", query)
			}
			if len(boundArguments) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(boundArguments), test.wantArgCount)
			}
		})
	}
}

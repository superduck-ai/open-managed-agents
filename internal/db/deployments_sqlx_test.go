package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

type stubNamedExecer struct {
	err    error
	query  string
	result sql.Result
	args   []any
}

func (s *stubNamedExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.query = query
	s.args = append([]any(nil), args...)
	return s.result, s.err
}

func (s *stubNamedExecer) Rebind(query string) string {
	return sqlx.Rebind(sqlx.DOLLAR, query)
}

type stubSQLResult struct {
	err          error
	rowsAffected int64
}

func (s stubSQLResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (s stubSQLResult) RowsAffected() (int64, error) {
	return s.rowsAffected, s.err
}

func TestDeploymentRunQueriesUseSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 23, 16, 0, 0, 0, time.UTC)
	run := DeploymentRun{
		UUID:                 "11111111-1111-4111-8111-111111111111",
		ExternalID:           "drun_test",
		OrganizationID:       1,
		WorkspaceID:          2,
		CreatedByAPIKeyID:    3,
		DeploymentID:         4,
		DeploymentExternalID: "dep_test",
		AgentID:              5,
		AgentExternalID:      "agent_test",
		AgentVersion:         1,
		AgentSnapshot:        []byte(`{"model":"test"}`),
		TriggerType:          "manual",
		TriggerContext:       []byte(`{"type":"manual"}`),
		CreatedAt:            now,
	}
	event := SessionEvent{
		UUID:              "22222222-2222-4222-8222-222222222222",
		ExternalID:        "sesevt_test",
		OrganizationID:    1,
		WorkspaceID:       2,
		SessionID:         6,
		SessionExternalID: "sesn_test",
		EventType:         "user.message",
		Payload:           []byte(`{"type":"user.message"}`),
		ProcessedAt:       now,
		CreatedAt:         now,
	}
	threadID := int64(7)
	threadExternalID := "sesthr_test"
	event.ThreadID = &threadID
	event.ThreadExternalID = &threadExternalID

	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:  "lock deployment",
			query: lockDeploymentForManualRunQuery,
			arguments: map[string]any{
				"workspace_id":           run.WorkspaceID,
				"deployment_external_id": run.DeploymentExternalID,
			},
			wantArgCount: 2,
		},
		{
			name:         "insert deployment run",
			query:        createDeploymentRunQuery,
			arguments:    deploymentRunArguments(run),
			wantArgCount: 16,
		},
		{
			name:  "update deployment timestamp",
			query: updateDeploymentLastRunQuery,
			arguments: map[string]any{
				"workspace_id":           run.WorkspaceID,
				"deployment_external_id": run.DeploymentExternalID,
				"last_run_at":            now,
			},
			wantArgCount: 4,
		},
		{
			name:  "lock session for events",
			query: lockSessionForEventsQuery,
			arguments: map[string]any{
				"workspace_id":        event.WorkspaceID,
				"session_external_id": event.SessionExternalID,
			},
			wantArgCount: 2,
		},
		{
			name:  "find primary thread",
			query: primarySessionThreadQuery,
			arguments: map[string]any{
				"workspace_id":        event.WorkspaceID,
				"session_external_id": event.SessionExternalID,
			},
			wantArgCount: 2,
		},
		{
			name:  "find explicit thread",
			query: sessionThreadByExternalIDQuery,
			arguments: map[string]any{
				"workspace_id":        event.WorkspaceID,
				"session_external_id": event.SessionExternalID,
				"thread_external_id":  threadExternalID,
			},
			wantArgCount: 3,
		},
		{
			name:         "insert session event",
			query:        createSessionEventQuery,
			arguments:    sessionEventArguments(event),
			wantArgCount: 12,
		},
		{
			name:         "insert idempotent session event",
			query:        createSessionEventIfAbsentQuery,
			arguments:    sessionEventArguments(event),
			wantArgCount: 12,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, test.query, test.arguments)
			if err != nil {
				t.Fatalf("bind named query: %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("query retains named parameter syntax: %q", query)
			}
			if len(arguments) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(arguments), test.wantArgCount)
			}
		})
	}
}

func TestUpdateDeploymentLastRunSQLX(t *testing.T) {
	now := time.Date(2026, time.July, 24, 8, 30, 0, 0, time.UTC)

	t.Run("propagates database exec error", func(t *testing.T) {
		wantErr := errors.New("boom")
		database := &stubNamedExecer{err: wantErr}

		err := updateDeploymentLastRunSQLX(context.Background(), database, 42, "dep_test", now)
		if !errors.Is(err, wantErr) {
			t.Fatalf("updateDeploymentLastRunSQLX() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("returns not found when zero rows were updated", func(t *testing.T) {
		database := &stubNamedExecer{result: stubSQLResult{rowsAffected: 0}}

		err := updateDeploymentLastRunSQLX(context.Background(), database, 42, "dep_missing", now)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("updateDeploymentLastRunSQLX() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("succeeds when at least one row was updated", func(t *testing.T) {
		database := &stubNamedExecer{result: stubSQLResult{rowsAffected: 1}}

		if err := updateDeploymentLastRunSQLX(context.Background(), database, 42, "dep_test", now); err != nil {
			t.Fatalf("updateDeploymentLastRunSQLX() error = %v, want nil", err)
		}
		if strings.Contains(database.query, ":") {
			t.Fatalf("bound query still contains named placeholders: %q", database.query)
		}
		if len(database.args) != 4 {
			t.Fatalf("bound argument count = %d, want 4", len(database.args))
		}
	})
}

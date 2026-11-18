package sessions

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestTitleEnqueueWarningKeepsSafePostgresDiagnostics(t *testing.T) {
	var logs bytes.Buffer
	handler := &Handler{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	handler.warnTitleEnqueueFailure(t.Context(), "session-fixture", fmt.Errorf("insert River job: %w", &pgconn.PgError{
		Code: "23514", ConstraintName: "river_job_test_constraint", Detail: "private job payload",
	}))
	diagnostic := logs.String()
	for _, field := range []string{`"level":"WARN"`, `"sql_state":"23514"`, `"constraint":"river_job_test_constraint"`} {
		if !strings.Contains(diagnostic, field) {
			t.Errorf("title enqueue warning is missing %s", field)
		}
	}
	if strings.Contains(diagnostic, "private job payload") {
		t.Error("title enqueue warning includes private job payload")
	}
}

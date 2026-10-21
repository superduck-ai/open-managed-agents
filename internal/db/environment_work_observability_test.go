package db

import (
	"database/sql/driver"
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestEnvironmentWorkTelemetryMapping(t *testing.T) {
	now := time.Date(2026, time.October, 15, 1, 2, 3, 0, time.UTC)
	parent := "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	for _, present := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy null fields", true: "populated fields"}[present], func(t *testing.T) {
			values := environmentWorkMapperTestRow()
			var queued *time.Time
			var traceParent *string
			if present {
				queued = &now
				traceParent = &parent
				values[len(values)-2] = now
				values[len(values)-1] = parent
			}
			executor := newMapperTestExecutor(t, mapperTestResponse{columns: environmentWorkMapperTestColumns(), rows: [][]driver.Value{values}})
			row, err := NewEnvironmentWorkMapper(executor).FindByExternalID(t.Context(), "workspace", "environment", "work")
			if err != nil {
				t.Fatal(err)
			}
			work := row.work()
			if !reflect.DeepEqual(work.QueuedAt, queued) {
				t.Fatalf("queued timestamp: %v", work.QueuedAt)
			}
			expected := ""
			if present {
				expected = parent
			}
			if work.TraceParent != expected {
				t.Fatalf("trace parent: %q", work.TraceParent)
			}
			params := environmentWorkWriteParamsFrom(EnvironmentWork{CreatedAt: now, TraceParent: expected})
			if !reflect.DeepEqual(params.TraceParent, traceParent) {
				t.Fatalf("nullable trace parent: %v", params.TraceParent)
			}
			bound := buildEnvironmentWorkMapperInsert(yourbatis.DialectPostgres, params)
			if len(bound.Args) != 18 || !reflect.DeepEqual(bound.Args[12].Value, now) || !reflect.DeepEqual(bound.Args[13].Value, traceParent) {
				t.Fatalf("insert telemetry bindings: %#v", bound.Args)
			}
			recovery := buildEnvironmentSandboxMapperScheduleRecoveryForCodeSession(yourbatis.DialectPostgres, environmentSandboxRecoveryParams{TraceParent: traceParent})
			if len(recovery.Args) != 3 || !reflect.DeepEqual(recovery.Args[2].Value, traceParent) {
				t.Fatalf("recovery bindings: %#v", recovery.Args)
			}
			if !containsMapperSQL(recovery.SQL, "trace_parent = COALESCE($3, work.trace_parent)") || !containsMapperSQL(recovery.SQL, "queued_at = NOW()") {
				t.Fatalf("recovery telemetry SQL: %s", recovery.SQL)
			}
		})
	}
}

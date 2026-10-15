package db

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestOperationalMetricsMapperErrors(t *testing.T) {
	calls := []mapperExecutionErrorContract{
		{statementID: "OperationalMetricsMapper.CountLiveSessionsByStatus", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, err := NewOperationalMetricsMapper(ex).CountLiveSessionsByStatus(context.Background())
			return err
		}},
		{statementID: "OperationalMetricsMapper.CountStaleRunningSessions", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, err := NewOperationalMetricsMapper(ex).CountStaleRunningSessions(context.Background(), time.Now())
			return err
		}},
		{statementID: "OperationalMetricsMapper.CountActiveCodeSessionsByWorkerState", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, err := NewOperationalMetricsMapper(ex).CountActiveCodeSessionsByWorkerState(context.Background())
			return err
		}},
		{statementID: "OperationalMetricsMapper.CountLiveEnvironmentWorkByState", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, err := NewOperationalMetricsMapper(ex).CountLiveEnvironmentWorkByState(context.Background())
			return err
		}},
		{statementID: "OperationalMetricsMapper.OldestQueuedEnvironmentWorkAgeSeconds", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, err := NewOperationalMetricsMapper(ex).OldestQueuedEnvironmentWorkAgeSeconds(context.Background())
			return err
		}},
		{statementID: "OperationalMetricsMapper.CountStuckStartingEnvironmentWork", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, err := NewOperationalMetricsMapper(ex).CountStuckStartingEnvironmentWork(context.Background(), time.Now())
			return err
		}},
		{statementID: "OperationalMetricsMapper.CountLiveEnvironmentSandboxesByState", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, err := NewOperationalMetricsMapper(ex).CountLiveEnvironmentSandboxesByState(context.Background())
			return err
		}},
	}
	for _, contract := range calls {
		t.Run(contract.statementID, func(t *testing.T) { assertMapperExecutionError(t, contract) })
	}
}

func TestOperationalMetricsMapperBindings(t *testing.T) {
	cutoff := time.Date(2026, time.October, 15, 0, 0, 0, 0, time.UTC)
	for _, contract := range []mapperBuilderContract{
		{statement: operationalMetricsMapperCountLiveSessionsByStatusStatement, bound: buildOperationalMetricsMapperCountLiveSessionsByStatus(yourbatis.DialectPostgres), wantID: "OperationalMetricsMapper.CountLiveSessionsByStatus", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{}, wantSQLFragments: []string{"deleted_at IS NULL", "archived_at IS NULL", "status IN ('running', 'rescheduling')", "GROUP BY status"}},
		{statement: operationalMetricsMapperCountStaleRunningSessionsStatement, bound: buildOperationalMetricsMapperCountStaleRunningSessions(yourbatis.DialectPostgres, cutoff), wantID: "OperationalMetricsMapper.CountStaleRunningSessions", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"staleBefore"}, wantSQLFragments: []string{"status = 'running'", "updated_at < $1", "archived_at IS NULL"}},
		{statement: operationalMetricsMapperCountActiveCodeSessionsByWorkerStateStatement, bound: buildOperationalMetricsMapperCountActiveCodeSessionsByWorkerState(yourbatis.DialectPostgres), wantID: "OperationalMetricsMapper.CountActiveCodeSessionsByWorkerState", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{}, wantSQLFragments: []string{"COALESCE(worker_status, 'unknown')", "status = 'active'", "GROUP BY COALESCE(worker_status, 'unknown'), connection_status"}},
		{statement: operationalMetricsMapperCountLiveEnvironmentWorkByStateStatement, bound: buildOperationalMetricsMapperCountLiveEnvironmentWorkByState(yourbatis.DialectPostgres), wantID: "OperationalMetricsMapper.CountLiveEnvironmentWorkByState", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{}, wantSQLFragments: []string{"state IN ('queued', 'starting', 'active', 'stopping')", "deleted_at IS NULL", "GROUP BY state"}},
		{statement: operationalMetricsMapperOldestQueuedEnvironmentWorkAgeSecondsStatement, bound: buildOperationalMetricsMapperOldestQueuedEnvironmentWorkAgeSeconds(yourbatis.DialectPostgres), wantID: "OperationalMetricsMapper.OldestQueuedEnvironmentWorkAgeSeconds", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{}, wantSQLFragments: []string{"MIN(COALESCE(queued_at, created_at))", "state = 'queued'", "deleted_at IS NULL", "COALESCE(queued_at, created_at) <= NOW()"}},
		{statement: operationalMetricsMapperCountStuckStartingEnvironmentWorkStatement, bound: buildOperationalMetricsMapperCountStuckStartingEnvironmentWork(yourbatis.DialectPostgres, cutoff), wantID: "OperationalMetricsMapper.CountStuckStartingEnvironmentWork", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"startedBefore"}, wantSQLFragments: []string{"state = 'starting'", "updated_at < $1"}},
		{statement: operationalMetricsMapperCountLiveEnvironmentSandboxesByStateStatement, bound: buildOperationalMetricsMapperCountLiveEnvironmentSandboxesByState(yourbatis.DialectPostgres), wantID: "OperationalMetricsMapper.CountLiveEnvironmentSandboxesByState", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{}, wantSQLFragments: []string{"state IN ('creating', 'running', 'stopping')", "GROUP BY state"}},
	} {
		t.Run(contract.wantID, func(t *testing.T) {
			assertMapperBuilderContract(t, contract)
			if len(contract.bound.Args) > 0 && !reflect.DeepEqual(contract.bound.Args[0].Value, cutoff) {
				t.Fatalf("cutoff binding: %v", contract.bound.Args)
			}
		})
	}
}

func TestOperationalMetricsMapperScanning(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"state", "total"}, rows: [][]driver.Value{{"running", "invalid"}}})
	if _, err := NewOperationalMetricsMapper(executor).CountLiveSessionsByStatus(t.Context()); err == nil {
		t.Fatal("invalid count accepted")
	}
	executor = newMapperTestExecutor(t, mapperTestResponse{columns: []string{"state", "total"}, rows: [][]driver.Value{{"running", int64(2)}}})
	rows, err := NewOperationalMetricsMapper(executor).CountLiveSessionsByStatus(t.Context())
	if err != nil || !reflect.DeepEqual(stateCounts(rows), []StateCount{{State: "running", Total: 2}}) {
		t.Fatalf("state counts: %v, %v", rows, err)
	}
	executor = newMapperTestExecutor(t, mapperTestResponse{columns: []string{"worker_status", "connection_status", "total"}, rows: [][]driver.Value{{"idle", "connected", int64(3)}}})
	codeRows, err := NewOperationalMetricsMapper(executor).CountActiveCodeSessionsByWorkerState(t.Context())
	if err != nil || !reflect.DeepEqual(codeSessionStateCounts(codeRows), []CodeSessionStateCount{{WorkerStatus: "idle", ConnectionStatus: "connected", Total: 3}}) {
		t.Fatalf("code session counts: %v, %v", codeRows, err)
	}
}

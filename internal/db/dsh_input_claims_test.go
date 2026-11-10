package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type inputClaimProbe struct {
	candidate  bool
	epoch      int64
	validLease bool
	exists     bool
	queueReads int
	steps      []string
	queries    []string
}
type inputClaimConnector struct{ probe *inputClaimProbe }

func (c inputClaimConnector) Connect(context.Context) (driver.Conn, error) {
	return &inputClaimConnection{probe: c.probe}, nil
}
func (c inputClaimConnector) Driver() driver.Driver { return inputClaimDriver{} }

type inputClaimDriver struct{}

func (inputClaimDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("connector required")
}

type inputClaimConnection struct{ probe *inputClaimProbe }

func (*inputClaimConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not supported")
}
func (*inputClaimConnection) Close() error { return nil }
func (c *inputClaimConnection) Begin() (driver.Tx, error) {
	c.probe.steps = append(c.probe.steps, "begin")
	return inputClaimTransaction{probe: c.probe}, nil
}

type inputClaimTransaction struct{ probe *inputClaimProbe }

func (tx inputClaimTransaction) Commit() error {
	tx.probe.steps = append(tx.probe.steps, "commit")
	return nil
}
func (tx inputClaimTransaction) Rollback() error {
	tx.probe.steps = append(tx.probe.steps, "rollback")
	return nil
}
func (c *inputClaimConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	q := strings.Join(strings.Fields(query), " ")
	c.probe.queries = append(c.probe.queries, q)
	now := time.Now().UTC()
	var row []driver.Value
	switch {
	case strings.Contains(q, "FROM sessions "):
		if !strings.Contains(q, "FOR UPDATE") {
			return nil, errors.New("expected session row lock")
		}
		c.probe.steps = append(c.probe.steps, "session-for-update")
		row = []driver.Value{"session-uuid", "ses_probe", "org", "workspace", nil, "env", "env_ext", "agent", "agt", int64(1), []byte(`{}`), nil, nil, nil, []byte(`{}`), nil, []byte(`[]`), "running", nil, nil, nil, []byte(`{}`), []byte(`{}`), nil, []byte(`{}`), now, now, nil, nil}
	case strings.Contains(q, "WITH worker AS MATERIALIZED"):
		c.probe.steps = append(c.probe.steps, "share-worker")
		row = []driver.Value{"code-uuid", int64(7), true}
	case strings.Contains(q, "AS lease_valid"):
		c.probe.steps = append(c.probe.steps, "read-worker")
		if !c.probe.exists {
			return &mapperTestRows{columns: []string{"uuid", "current_worker_epoch", "lease_valid"}}, nil
		}
		row = []driver.Value{"code-uuid", c.probe.epoch, c.probe.validLease}
	case strings.Contains(q, "FROM code_sessions "):
		c.probe.steps = append(c.probe.steps, "find-code")
		row = []driver.Value{"code-uuid", "cse_probe", "ses_probe"}
	case strings.Contains(q, "FROM session_threads "):
		c.probe.steps = append(c.probe.steps, "primary-thread")
		row = []driver.Value{"thread-uuid", "sthr_probe", "org", "workspace", "session-uuid", "ses_probe", nil, nil, []byte(`{}`), "running", []byte(`{}`), []byte(`{}`), nil, false, false, now, now, nil, nil}
	case strings.Contains(q, "FROM dsh_input_executions "):
		c.probe.queueReads++
		if c.probe.candidate && c.probe.queueReads == 1 {
			c.probe.steps = append(c.probe.steps, "candidate")
			return &mapperTestRows{columns: []string{"input_id", "session_thread_id", "status", "result", "failure_code", "accepted_at", "started_at", "completed_at", "cancel_requested_at"}, rows: [][]driver.Value{{"input", "sthr_probe", "accepted", nil, nil, now, nil, nil, nil}}}, nil
		}
		c.probe.steps = append(c.probe.steps, "next-execution-empty")
		return &mapperTestRows{columns: []string{"input_id"}}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", q)
	}
	columns := make([]string, len(row))
	for i := range columns {
		columns[i] = fmt.Sprint(i)
	}
	return &mapperTestRows{columns: columns, rows: [][]driver.Value{row}}, nil
}

func inputClaimStore(t *testing.T, probe *inputClaimProbe) *DB {
	t.Helper()
	standard := sql.OpenDB(inputClaimConnector{probe: probe})
	t.Cleanup(func() { _ = standard.Close() })
	return &DB{mapperDB: yourbatis.NewDB(standard, yourbatis.DialectPostgres)}
}
func inputClaimScope() DSHScope {
	return DSHScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code-uuid", CodeSessionID: "cse_probe", SessionID: "ses_probe", Epoch: 7}
}
func TestEmptyInputClaimStillValidatesCurrentWorker(t *testing.T) {
	for _, item := range []struct {
		name          string
		epoch         int64
		lease, exists bool
		want          error
	}{
		{"fenced", 8, true, true, ErrWorkerEpochMismatch},
		{"expired", 7, false, true, ErrWorkerLeaseExpired},
		{"missing", 7, true, false, ErrNotFound},
	} {
		t.Run(item.name, func(t *testing.T) {
			probe := &inputClaimProbe{epoch: item.epoch, validLease: item.lease, exists: item.exists}
			_, _, err := inputClaimStore(t, probe).ClaimDSHInput(t.Context(), inputClaimScope())
			if !errors.Is(err, item.want) {
				t.Fatalf("err=%v want=%v", err, item.want)
			}
			if strings.Join(probe.steps, ",") != "next-execution-empty,read-worker" {
				t.Fatalf("steps=%v", probe.steps)
			}
		})
	}
}
func TestEmptyInputClaimAvoidsTransactionAndSessionLock(t *testing.T) {
	probe := &inputClaimProbe{epoch: 7, validLease: true, exists: true}
	claim, changes, err := inputClaimStore(t, probe).ClaimDSHInput(t.Context(), inputClaimScope())
	if err != nil || claim.Event.ExternalID != "" || len(changes.Events) != 0 {
		t.Fatalf("claim=%+v changes=%+v err=%v", claim, changes, err)
	}
	if strings.Join(probe.steps, ",") != "next-execution-empty,read-worker" {
		t.Fatalf("steps=%v", probe.steps)
	}
	for _, query := range probe.queries {
		if strings.Contains(query, "FOR UPDATE") || strings.Contains(query, "FOR KEY SHARE") {
			t.Fatalf("unexpected row lock: %s", query)
		}
	}
}
func TestInputClaimRechecksCandidateUnderSessionLock(t *testing.T) {
	probe := &inputClaimProbe{candidate: true, epoch: 7, validLease: true, exists: true}
	claim, _, err := inputClaimStore(t, probe).ClaimDSHInput(t.Context(), inputClaimScope())
	if err != nil || claim.Event.ExternalID != "" {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	want := "candidate,begin,session-for-update,find-code,share-worker,primary-thread,next-execution-empty,commit"
	if strings.Join(probe.steps, ",") != want {
		t.Fatalf("steps=%v want=%s", probe.steps, want)
	}
}
func TestReadWorkerMapperDoesNotLockRows(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: dSHTranscriptMapperReadWorkerStatement,
		bound:     buildDSHTranscriptMapperReadWorker(yourbatis.DialectPostgres, inputClaimScope()),
		wantID:    "DSHTranscriptMapper.ReadWorker", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"},
		wantSQLFragments:  []string{"FROM code_sessions", "AS lease_valid", "status = 'active'", "deleted_at IS NULL"},
		wantSQLAbsent:     []string{"FOR UPDATE", "FOR KEY SHARE"},
	})
}

package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

type permissionReadConnector struct {
	probe *inputClaimProbe
	row   []driver.Value
}

func (c permissionReadConnector) Driver() driver.Driver { return inputClaimDriver{} }
func (c permissionReadConnector) Connect(context.Context) (driver.Conn, error) {
	return &permissionReadConnection{inputClaimConnection: &inputClaimConnection{probe: c.probe}, row: c.row}, nil
}

type permissionReadConnection struct {
	*inputClaimConnection
	row []driver.Value
}

func (c *permissionReadConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(query, "FROM dsh_permission_requests") {
		return c.inputClaimConnection.QueryContext(ctx, query, args)
	}
	c.probe.steps = append(c.probe.steps, "read-permission")
	if strings.Contains(query, "FOR UPDATE") {
		return nil, fmt.Errorf("unexpected lock: %s", query)
	}
	columns := make([]string, 12)
	for i := range columns {
		columns[i] = fmt.Sprint(i)
	}
	rows := [][]driver.Value{}
	if c.row != nil {
		rows = append(rows, c.row)
	}
	return &mapperTestRows{columns: columns, rows: rows}, nil
}
func permissionReadStore(t *testing.T, probe *inputClaimProbe, row []driver.Value) *DB {
	t.Helper()
	standard := sql.OpenDB(permissionReadConnector{probe: probe, row: row})
	t.Cleanup(func() { _ = standard.Close() })
	return &DB{mapperDB: yourbatis.NewDB(standard, yourbatis.DialectPostgres)}
}
func permissionReadRequest() DSHPermission {
	return DSHPermission{RequestID: "pr_one", InputID: "input", DSHSessionID: "dsh", ToolUseID: "call", Turn: "1", Tool: []byte(`{"name":"read"}`), Input: []byte(`{"path":"a"}`)}
}
func permissionReadRow() []driver.Value {
	return []driver.Value{"pr_one", "input", "dsh", "thread", "call", "1", []byte(`{"name":"read"}`), []byte(`{"path":"a"}`), nil, "", "pending", nil}
}
func TestReadPermissionRejectsInvalidWorker(t *testing.T) {
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
			_, _, err := permissionReadStore(t, probe, permissionReadRow()).ReadDSHPermission(t.Context(), inputClaimScope(), permissionReadRequest())
			if !errors.Is(err, item.want) || strings.Join(probe.steps, ",") != "read-worker" {
				t.Fatalf("err=%v steps=%v", err, probe.steps)
			}
		})
	}
}
func TestReadPermissionRejectsChangedIdentityOrContent(t *testing.T) {
	for _, field := range []string{"input", "session", "call", "turn", "tool", "arguments", "available"} {
		t.Run(field, func(t *testing.T) {
			request := permissionReadRequest()
			switch field {
			case "input":
				request.InputID = "other"
			case "session":
				request.DSHSessionID = "other"
			case "call":
				request.ToolUseID = "other"
			case "turn":
				request.Turn = "2"
			case "tool":
				request.Tool = []byte(`{"name":"write"}`)
			case "arguments":
				request.Input = []byte(`{"path":"b"}`)
			case "available":
				value := true
				request.Available = &value
			}
			probe := &inputClaimProbe{epoch: 7, validLease: true, exists: true}
			_, _, err := permissionReadStore(t, probe, permissionReadRow()).ReadDSHPermission(t.Context(), inputClaimScope(), request)
			if !errors.Is(err, ErrDSHPermissionConflict) {
				t.Fatalf("err=%v", err)
			}
			if strings.Join(probe.steps, ",") != "read-worker,read-permission" {
				t.Fatalf("steps=%v", probe.steps)
			}
		})
	}
}
func TestReadPermissionAvoidsTransactionsForEveryState(t *testing.T) {
	for _, state := range []string{"pending", "resolved", "cancelled", "closed", "tombstone", "absent"} {
		t.Run(state, func(t *testing.T) {
			row := permissionReadRow()
			row[10] = state
			switch state {
			case "resolved":
				row[11] = []byte(`{"behavior":"allow"}`)
			case "tombstone":
				row[1] = nil
				row[5] = nil
				row[10] = "cancelled"
			case "absent":
				row = nil
			}
			probe := &inputClaimProbe{epoch: 7, validLease: true, exists: true}
			result, found, err := permissionReadStore(t, probe, row).ReadDSHPermission(t.Context(), inputClaimScope(), permissionReadRequest())
			if err != nil || found != (state != "absent") {
				t.Fatalf("result=%+v found=%v err=%v", result, found, err)
			}
			if found && result.State != row[10] {
				t.Fatalf("state=%v", result.State)
			}
			if strings.Join(probe.steps, ",") != "read-worker,read-permission" {
				t.Fatalf("steps=%v", probe.steps)
			}
		})
	}
}

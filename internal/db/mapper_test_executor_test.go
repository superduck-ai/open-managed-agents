package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

type mapperTestResponse struct {
	columns      []string
	rows         [][]driver.Value
	queryErr     error
	execErr      error
	rowsAffected int64
}

type mapperTestExecutor struct {
	database       *sql.DB
	response       mapperTestResponse
	statement      yourbatis.Statement
	bound          yourbatis.BoundSQL
	queryCallCount int
	execCallCount  int
}

func newMapperTestExecutor(t *testing.T, response mapperTestResponse) *mapperTestExecutor {
	t.Helper()
	database := sql.OpenDB(mapperTestConnector{response: response})
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close mapper test database: %v", err)
		}
	})
	return &mapperTestExecutor{database: database, response: response}
}

func (executor *mapperTestExecutor) Dialect() yourbatis.Dialect {
	return yourbatis.DialectPostgres
}

func (executor *mapperTestExecutor) Query(
	ctx context.Context,
	statement yourbatis.Statement,
	bound yourbatis.BoundSQL,
) (*sql.Rows, error) {
	executor.queryCallCount++
	executor.statement = statement
	executor.bound = bound
	if executor.response.queryErr != nil {
		return nil, executor.response.queryErr
	}
	return executor.database.QueryContext(ctx, "SELECT mapper_test")
}

func (executor *mapperTestExecutor) Exec(
	_ context.Context,
	statement yourbatis.Statement,
	bound yourbatis.BoundSQL,
) (sql.Result, error) {
	executor.execCallCount++
	executor.statement = statement
	executor.bound = bound
	if executor.response.execErr != nil {
		return nil, executor.response.execErr
	}
	return driver.RowsAffected(executor.response.rowsAffected), nil
}

func assertMapperTestExecution(
	t *testing.T,
	executor *mapperTestExecutor,
	statementID string,
	kind yourbatis.StatementKind,
	wantValues []any,
	wantSQLFragments ...string,
) {
	t.Helper()
	if calls := executor.queryCallCount + executor.execCallCount; calls != 1 {
		t.Fatalf("mapper executor call count = %d, want 1", calls)
	}
	if executor.statement.ID != statementID || executor.statement.Kind != kind {
		t.Fatalf("mapper statement = %+v, want ID %q and kind %q", executor.statement, statementID, kind)
	}
	if executor.statement.Source == "" {
		t.Fatal("mapper statement source is empty")
	}
	if values := executor.bound.Values(); !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("mapper arguments = %#v, want %#v", values, wantValues)
	}
	for _, fragment := range wantSQLFragments {
		if !strings.Contains(executor.bound.SQL, fragment) {
			t.Fatalf("mapper SQL = %q, want fragment %q", executor.bound.SQL, fragment)
		}
	}
}

type mapperTestConnector struct {
	response mapperTestResponse
}

func (connector mapperTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &mapperTestConnection{response: connector.response}, nil
}

func (connector mapperTestConnector) Driver() driver.Driver {
	return mapperTestDriver{}
}

type mapperTestDriver struct{}

func (mapperTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("mapper test driver requires sql.OpenDB")
}

type mapperTestConnection struct {
	response mapperTestResponse
}

func (*mapperTestConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("mapper test driver does not prepare statements")
}

func (*mapperTestConnection) Close() error {
	return nil
}

func (*mapperTestConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("mapper test driver does not begin transactions")
}

func (connection *mapperTestConnection) QueryContext(
	context.Context,
	string,
	[]driver.NamedValue,
) (driver.Rows, error) {
	return &mapperTestRows{
		columns: connection.response.columns,
		rows:    connection.response.rows,
	}, nil
}

type mapperTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (rows *mapperTestRows) Columns() []string {
	return rows.columns
}

func (*mapperTestRows) Close() error {
	return nil
}

func (rows *mapperTestRows) Next(values []driver.Value) error {
	if rows.index >= len(rows.rows) {
		return io.EOF
	}
	copy(values, rows.rows[rows.index])
	rows.index++
	return nil
}

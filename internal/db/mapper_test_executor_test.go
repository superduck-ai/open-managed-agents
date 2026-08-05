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

type mapperBuilderContract struct {
	statement                  yourbatis.Statement
	bound                      yourbatis.BoundSQL
	wantID                     string
	wantKind                   yourbatis.StatementKind
	wantArgumentNames          []string
	wantSensitiveArgumentNames []string
	wantSQLFragments           []string
}

type mapperExecutionErrorContract struct {
	statementID string
	kind        yourbatis.StatementKind
	query       bool
	call        func(yourbatis.Executor) error
}

func assertMapperBuilderContract(t *testing.T, contract mapperBuilderContract) {
	t.Helper()
	if contract.statement.ID != contract.wantID || contract.statement.Kind != contract.wantKind {
		t.Fatalf(
			"mapper statement = %+v, want ID %q and kind %q",
			contract.statement,
			contract.wantID,
			contract.wantKind,
		)
	}
	if contract.statement.Source == "" {
		t.Fatal("mapper statement source is empty")
	}
	argumentNames := make([]string, 0, len(contract.bound.Args))
	var sensitiveArgumentNames []string
	for _, argument := range contract.bound.Args {
		argumentNames = append(argumentNames, argument.Name)
		if argument.Sensitive {
			sensitiveArgumentNames = append(sensitiveArgumentNames, argument.Name)
		}
	}
	if !reflect.DeepEqual(argumentNames, contract.wantArgumentNames) {
		t.Fatalf("mapper argument names = %#v, want %#v", argumentNames, contract.wantArgumentNames)
	}
	if !reflect.DeepEqual(sensitiveArgumentNames, contract.wantSensitiveArgumentNames) {
		t.Fatalf(
			"mapper sensitive argument names = %#v, want %#v",
			sensitiveArgumentNames,
			contract.wantSensitiveArgumentNames,
		)
	}
	if strings.Contains(contract.bound.SQL, "#{") || strings.Contains(contract.bound.SQL, "::") {
		t.Fatalf("mapper SQL retains unsupported placeholder or cast syntax: %q", contract.bound.SQL)
	}
	for _, fragment := range contract.wantSQLFragments {
		if !containsMapperSQL(contract.bound.SQL, fragment) {
			t.Fatalf("mapper SQL = %q, want fragment %q", contract.bound.SQL, fragment)
		}
	}
}

func assertMapperExecutionError(t *testing.T, contract mapperExecutionErrorContract) {
	t.Helper()
	executionErr := errors.New("mapper execution failed")
	response := mapperTestResponse{execErr: executionErr}
	if contract.query {
		response = mapperTestResponse{queryErr: executionErr}
	}
	executor := newMapperTestExecutor(t, response)
	if err := contract.call(executor); !errors.Is(err, executionErr) {
		t.Fatalf("mapper error = %v, want %v", err, executionErr)
	}
	assertMapperTestExecution(t, executor, contract.statementID, contract.kind, executor.bound.Values())
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
		if !containsMapperSQL(executor.bound.SQL, fragment) {
			t.Fatalf("mapper SQL = %q, want fragment %q", executor.bound.SQL, fragment)
		}
	}
}

func containsMapperSQL(sql, fragment string) bool {
	return strings.Contains(
		strings.Join(strings.Fields(sql), " "),
		strings.Join(strings.Fields(fragment), " "),
	)
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

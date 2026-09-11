package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestMCPTunnelTokenMapperOptionalRows(t *testing.T) {
	for _, query := range []struct {
		name    string
		call    func(MCPTunnelTokenMapper) (string, bool, error)
		values  []any
		clauses []string
	}{
		{
			name: "FindActiveByTunnelUUID",
			call: func(mapper MCPTunnelTokenMapper) (string, bool, error) {
				row, found, err := mapper.FindActiveByTunnelUUID(t.Context(), "tunnel-uuid")
				return row.UUID, found, err
			},
			values: []any{"tunnel-uuid"}, clauses: []string{"tunnel_uuid = $1", "retired_at IS NULL", "archived_at IS NULL"},
		},
		{
			name: "FindActiveForUpdate",
			call: func(mapper MCPTunnelTokenMapper) (string, bool, error) {
				row, found, err := mapper.FindActiveForUpdate(t.Context(), "tunnel-uuid")
				return row.UUID, found, err
			},
			values: []any{"tunnel-uuid"}, clauses: []string{"tunnel_uuid = $1", "retired_at IS NULL", "archived_at IS NULL", "FOR UPDATE"},
		},
		{
			name: "FindByHashAndTunnelExternalID",
			call: func(mapper MCPTunnelTokenMapper) (string, bool, error) {
				row, found, err := mapper.FindByHashAndTunnelExternalID(t.Context(), []byte("hash"), "tunnel-external")
				return row.UUID, found, err
			},
			values: []any{[]byte("hash"), "tunnel-external"}, clauses: []string{"JOIN mcp_tunnels", "tv.token_hash = $1", "t.external_id = $2"},
		},
	} {
		t.Run(query.name, func(t *testing.T) {
			columns := []string{"uuid", "external_id", "tunnel_uuid", "version", "token_hash", "ciphertext", "nonce", "wrapped_dek", "format_version", "key_provider", "key_version", "created_at", "retired_at", "archived_at"}
			row := []driver.Value{"token-uuid", "token-external", "tunnel-uuid", int64(1), []byte("hash"), nil, nil, nil, nil, nil, nil, time.Now().UTC(), nil, nil}
			if query.name == "FindByHashAndTunnelExternalID" {
				columns = append(columns, "tunnel_external_id", "organization_uuid", "workspace_uuid", "tunnel_archived_at")
				row = append(row, "tunnel-external", "organization", "workspace", nil)
			}
			badRow := append([]driver.Value(nil), row...)
			badRow[3] = "invalid-version"
			queryErr := errors.New("database unavailable")
			for _, test := range []struct {
				name     string
				response mapperTestResponse
				wantErr  bool
				found    bool
			}{
				{name: "query error", response: mapperTestResponse{queryErr: queryErr}, wantErr: true},
				{name: "scan error", response: mapperTestResponse{columns: columns, rows: [][]driver.Value{badRow}}, wantErr: true},
				{name: "not found", response: mapperTestResponse{columns: columns}},
				{name: "found", response: mapperTestResponse{columns: columns, rows: [][]driver.Value{row}}, found: true},
			} {
				t.Run(test.name, func(t *testing.T) {
					executor := newMapperTestExecutor(t, test.response)
					id, found, err := query.call(NewMCPTunnelTokenMapper(executor))
					if (err != nil) != test.wantErr || found != test.found {
						t.Fatalf("result = (%q, %t, %v), want found=%t, error=%t", id, found, err, test.found, test.wantErr)
					}
					if test.response.queryErr != nil && !errors.Is(err, queryErr) {
						t.Fatalf("query error was lost: %v", err)
					}
					if test.found && id != "token-uuid" {
						t.Fatalf("unexpected token UUID: %q", id)
					}
					assertMapperTestExecution(t, executor, "MCPTunnelTokenMapper."+query.name, yourbatis.StatementSelect, query.values, query.clauses...)
				})
			}
		})
	}
}

func TestMCPTunnelTokenQueriesPreserveDatabaseFailures(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{})
	database := &DB{mapperDB: yourbatis.NewDB(executor.database, yourbatis.DialectPostgres)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := database.GetActiveMCPTunnelToken(ctx, "organization", "workspace", "tunnel")
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrNotFound) {
		t.Fatalf("GetActiveMCPTunnelToken masked database failure: %v", err)
	}
	_, err = database.FindMCPTunnelTokenContext(ctx, "tunnel", []byte("hash"))
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrNotFound) {
		t.Fatalf("FindMCPTunnelTokenContext masked database failure: %v", err)
	}
}

func TestMCPTunnelTokenContextRetainsNotFoundContract(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{})
	database := &DB{mapperDB: yourbatis.NewDB(executor.database, yourbatis.DialectPostgres)}
	_, err := database.FindMCPTunnelTokenContext(t.Context(), "tunnel", []byte("hash"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing token error = %v, want ErrNotFound", err)
	}
}

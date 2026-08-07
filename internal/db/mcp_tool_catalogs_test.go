package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestMCPToolCatalogMapperStatements(t *testing.T) {
	tools := []byte(`[{"name":"get_weather"}]`)
	params := upsertMCPToolCatalogParams{
		ExternalID:    "mcpc_test",
		TransportType: "url",
		EndpointURL:   "https://mcp.example.test/mcp",
		Tools:         tools,
	}
	tests := []struct {
		name      string
		statement yourbatis.Statement
		bound     yourbatis.BoundSQL
		id        string
		kind      yourbatis.StatementKind
		values    []any
		fragments []string
	}{
		{
			name:      "find by endpoint",
			statement: mCPToolCatalogMapperFindByEndpointStatement,
			bound:     buildMCPToolCatalogMapperFindByEndpoint(yourbatis.DialectPostgres, params.TransportType, params.EndpointURL),
			id:        "MCPToolCatalogMapper.FindByEndpoint",
			kind:      yourbatis.StatementSelect,
			values:    []any{params.TransportType, params.EndpointURL},
			fragments: []string{"FROM mcp_tool_catalogs", "transport_type = $1", "endpoint_url = $2"},
		},
		{
			name:      "upsert",
			statement: mCPToolCatalogMapperUpsertStatement,
			bound:     buildMCPToolCatalogMapperUpsert(yourbatis.DialectPostgres, params),
			id:        "MCPToolCatalogMapper.Upsert",
			kind:      yourbatis.StatementInsert,
			values:    []any{params.ExternalID, params.TransportType, params.EndpointURL, tools},
			fragments: []string{"INSERT INTO mcp_tool_catalogs", "CAST($4 AS jsonb)", "ON CONFLICT", "RETURNING"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.statement.ID != test.id || test.statement.Kind != test.kind || test.statement.Source == "" {
				t.Fatalf("statement = %+v, want ID %q, kind %q, and source", test.statement, test.id, test.kind)
			}
			if values := test.bound.Values(); !reflect.DeepEqual(values, test.values) {
				t.Fatalf("values = %#v, want %#v", values, test.values)
			}
			if strings.Contains(test.bound.SQL, "#{") || strings.Contains(test.bound.SQL, "::") {
				t.Fatalf("SQL retains unsupported syntax: %q", test.bound.SQL)
			}
			for _, fragment := range test.fragments {
				if !strings.Contains(test.bound.SQL, fragment) {
					t.Fatalf("SQL = %q, want fragment %q", test.bound.SQL, fragment)
				}
			}
			for _, argument := range test.bound.Args {
				wantSensitive := argument.Name == "endpointURL" || argument.Name == "params.EndpointURL"
				if argument.Sensitive != wantSensitive {
					t.Fatalf("argument %q sensitive = %t, want %t", argument.Name, argument.Sensitive, wantSensitive)
				}
			}
		})
	}
}

func TestMCPToolCatalogMapperResultSemantics(t *testing.T) {
	ctx := context.Background()

	t.Run("find scans string UUID", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: mcpToolCatalogMapperTestColumns(),
			rows:    [][]driver.Value{mcpToolCatalogMapperTestRow()},
		})
		row, err := NewMCPToolCatalogMapper(executor).FindByEndpoint(ctx, "url", "https://mcp.example.test/mcp")
		catalog, mapErr := row.catalog()
		if err != nil || mapErr != nil || catalog.UUID != "00000000-0000-4000-8000-000000000001" || len(catalog.Tools) != 1 {
			t.Fatalf("FindByEndpoint() = (%+v, %v, %v)", catalog, err, mapErr)
		}
	})

	t.Run("upsert returns row", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: mcpToolCatalogMapperTestColumns(),
			rows:    [][]driver.Value{mcpToolCatalogMapperTestRow()},
		})
		row, err := NewMCPToolCatalogMapper(executor).Upsert(ctx, upsertMCPToolCatalogParams{})
		if err != nil || row.ExternalID != "mcpc_test" {
			t.Fatalf("Upsert() = (%+v, %v)", row, err)
		}
	})

	t.Run("zero rows preserves sql ErrNoRows", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: mcpToolCatalogMapperTestColumns()})
		_, err := NewMCPToolCatalogMapper(executor).FindByEndpoint(ctx, "url", "https://mcp.example.test/missing")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindByEndpoint() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("scan error is returned", func(t *testing.T) {
		row := mcpToolCatalogMapperTestRow()
		row[0] = "invalid-id"
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: mcpToolCatalogMapperTestColumns(),
			rows:    [][]driver.Value{row},
		})
		if _, err := NewMCPToolCatalogMapper(executor).FindByEndpoint(ctx, "url", "https://mcp.example.test/mcp"); err == nil {
			t.Fatal("FindByEndpoint() scan error = nil")
		}
	})
}

func TestMCPToolCatalogMapperMethodsPropagateExecutionErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{
			name: "find by endpoint",
			contract: mapperExecutionErrorContract{
				statementID: "MCPToolCatalogMapper.FindByEndpoint",
				kind:        yourbatis.StatementSelect,
				query:       true,
				call: func(executor yourbatis.Executor) error {
					_, err := NewMCPToolCatalogMapper(executor).FindByEndpoint(ctx, "url", "https://mcp.example.test/mcp")
					return err
				},
			},
		},
		{
			name: "upsert",
			contract: mapperExecutionErrorContract{
				statementID: "MCPToolCatalogMapper.Upsert",
				kind:        yourbatis.StatementInsert,
				query:       true,
				call: func(executor yourbatis.Executor) error {
					_, err := NewMCPToolCatalogMapper(executor).Upsert(ctx, upsertMCPToolCatalogParams{})
					return err
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperExecutionError(t, test.contract)
		})
	}
}

func mcpToolCatalogMapperTestColumns() []string {
	return []string{"id", "uuid", "external_id", "transport_type", "endpoint_url", "tools", "created_at", "updated_at"}
}

func mcpToolCatalogMapperTestRow() []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		int64(1), "00000000-0000-4000-8000-000000000001", "mcpc_test", "url",
		"https://mcp.example.test/mcp", []byte(`[{"name":"get_weather"}]`), now, now,
	}
}

func TestUpsertMCPToolCatalogRejectsNilTools(t *testing.T) {
	database := &DB{}
	if _, err := database.UpsertMCPToolCatalog(context.Background(), "url", "https://mcp.example.test/mcp", nil); err == nil {
		t.Fatal("UpsertMCPToolCatalog with nil tools succeeded, want error")
	}
}

func TestMCPToolCatalogToolsJSONSemantics(t *testing.T) {
	t.Run("SQL NULL is rejected because rows only contain successful snapshots", func(t *testing.T) {
		if _, err := decodeMCPToolCatalogTools(nil); err == nil {
			t.Fatal("decode nil tools succeeded, want array error")
		}
	})

	t.Run("JSON empty array remains known empty", func(t *testing.T) {
		tools, err := decodeMCPToolCatalogTools([]byte(`[]`))
		if err != nil {
			t.Fatalf("decode empty tools: %v", err)
		}
		if tools == nil || len(tools) != 0 {
			t.Fatalf("decoded empty tools = %#v, want non-nil empty slice", tools)
		}
		encoded, err := encodeMCPToolCatalogTools(tools)
		if err != nil {
			t.Fatalf("encode empty tools: %v", err)
		}
		if string(encoded) != `[]` {
			t.Fatalf("encoded empty tools = %s, want []", encoded)
		}
	})

	t.Run("typed fields round trip", func(t *testing.T) {
		const raw = `[{"name":"get_weather","title":"Get weather","description":"Returns a forecast."}]`
		tools, err := decodeMCPToolCatalogTools([]byte(raw))
		if err != nil {
			t.Fatalf("decode populated tools: %v", err)
		}
		if len(tools) != 1 || tools[0].Name != "get_weather" || tools[0].Title != "Get weather" || tools[0].Description != "Returns a forecast." {
			t.Fatalf("decoded tools = %#v", tools)
		}
		encoded, err := encodeMCPToolCatalogTools(tools)
		if err != nil {
			t.Fatalf("encode populated tools: %v", err)
		}
		if string(encoded) != raw {
			t.Fatalf("encoded tools = %s, want %s", encoded, raw)
		}
	})

	t.Run("nil cannot be completed as success", func(t *testing.T) {
		if _, err := encodeMCPToolCatalogTools(nil); err == nil {
			t.Fatal("encode nil tools succeeded, want error")
		}
	})

	t.Run("JSON null is rejected", func(t *testing.T) {
		if _, err := decodeMCPToolCatalogTools([]byte(`null`)); err == nil {
			t.Fatal("decode JSON null succeeded, want array error")
		}
	})
}

package db

import (
	"context"
	"testing"
)

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

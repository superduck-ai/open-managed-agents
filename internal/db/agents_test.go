package db

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestAgentMapperListPageBindsStringUUIDParameters(t *testing.T) {
	createdAtGTE := time.Date(2026, time.July, 20, 1, 2, 3, 0, time.UTC)
	createdAtLTE := createdAtGTE.Add(24 * time.Hour)
	cursorCreatedAt := createdAtGTE.Add(12 * time.Hour)
	bound := buildAgentMapperListPage(yourbatis.DialectPostgres, agentPageFilter{
		WorkspaceUUID:   "00000000-0000-0000-0000-000000000042",
		Name:            "managed",
		Limit:           6,
		Cursor:          &AgentPageCursor{CreatedAt: cursorCreatedAt, UUID: "00000000-0000-0000-0000-000000000017"},
		IncludeArchived: true,
		CreatedAtGTE:    &createdAtGTE,
		CreatedAtLTE:    &createdAtLTE,
	})

	wantValues := []any{
		"00000000-0000-0000-0000-000000000042",
		&createdAtGTE,
		&createdAtLTE,
		"managed",
		cursorCreatedAt,
		"00000000-0000-0000-0000-000000000017",
		6,
	}
	if values := bound.Values(); !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("mapper values = %#v, want %#v", values, wantValues)
	}
	if strings.Contains(bound.SQL, "CAST($1 AS uuid)") || strings.Contains(bound.SQL, "CAST($6 AS uuid)") {
		t.Fatalf("mapper query contains UUID parameter cast ceremony: %q", bound.SQL)
	}
	for _, fragment := range []string{
		"created_at >= $2",
		"created_at <= $3",
		"POSITION(LOWER($4) IN LOWER(name)) > 0",
		"(created_at, uuid) < ($5, $6)",
		"LIMIT $7",
	} {
		if !strings.Contains(bound.SQL, fragment) {
			t.Fatalf("mapper query = %q, want fragment %q", bound.SQL, fragment)
		}
	}
}

func TestAgentMapperInsertBindsJSONArguments(t *testing.T) {
	createdAt := time.Date(2026, time.July, 23, 4, 5, 6, 0, time.UTC)
	agent := Agent{
		UUID:                "4b01277d-4904-43c6-8d6a-3d866637d540",
		ExternalID:          "agent_mapper",
		WorkspaceUUID:       "00000000-0000-0000-0000-000000000011",
		CreatedByAPIKeyUUID: "00000000-0000-0000-0000-000000000012",
		Name:                "Mapper agent",
		Model:               json.RawMessage(`{"id":"claude-opus-4-6"}`),
		MCPServers:          json.RawMessage(`[]`),
		Metadata:            json.RawMessage(`{"source":"test"}`),
		Skills:              json.RawMessage(`[]`),
		Tools:               json.RawMessage(`[]`),
		CreatedAt:           createdAt,
	}

	bound := buildAgentMapperInsert(yourbatis.DialectPostgres, insertAgentParams{
		UUID:                agent.UUID,
		ExternalID:          agent.ExternalID,
		WorkspaceUUID:       agent.WorkspaceUUID,
		CreatedByAPIKeyUUID: nullableString(agent.CreatedByAPIKeyUUID),
		Config:              newAgentConfigParams(agent),
		CreatedAt:           createdAt,
	})
	values := bound.Values()
	if !strings.Contains(bound.SQL, "CAST($8 AS jsonb)") {
		t.Fatalf("mapper query JSON cast = %q, want model bound as PostgreSQL parameter $8", bound.SQL)
	}
	if len(values) != 15 {
		t.Fatalf("mapper value count = %d, want 15", len(values))
	}
	if model, ok := values[7].([]byte); !ok || !bytes.Equal(model, agent.Model) {
		t.Fatalf("bound model = %#v, want %s", values[7], agent.Model)
	}
	if multiagent, ok := values[10].([]byte); !ok || multiagent != nil {
		t.Fatalf("bound multiagent = %#v, want nil JSON bytes", values[10])
	}
	for index, argument := range bound.Args {
		wantSensitive := index >= 5 && index <= 12 && index != 4
		if argument.Sensitive != wantSensitive {
			t.Fatalf("argument %q sensitive = %t, want %t", argument.Name, argument.Sensitive, wantSensitive)
		}
	}
}

func TestAgentRowConversionCopiesJSON(t *testing.T) {
	row := agentRow{
		UUID:           "00000000-0000-0000-0000-000000000007",
		ExternalID:     "agent_row",
		CurrentVersion: 2,
		Name:           "row agent",
		Model:          []byte(`{"id":"claude-opus-4-6"}`),
		MCPServers:     []byte(`[]`),
		Metadata:       []byte(`{"source":"row"}`),
		Skills:         []byte(`[]`),
		Tools:          []byte(`[]`),
	}

	agent := row.agent()
	row.Model[0] = '['

	if string(agent.Model) != `{"id":"claude-opus-4-6"}` {
		t.Fatalf("agent.Model = %s, want copied JSON", agent.Model)
	}
	if agent.UUID != row.UUID || agent.ExternalID != row.ExternalID || agent.CurrentVersion != row.CurrentVersion {
		t.Fatalf("agent identity fields = %+v, want values from row %+v", agent, row)
	}
}

package db

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAgentPageQueryBindsNamedParameters(t *testing.T) {
	createdAtGTE := time.Date(2026, time.July, 20, 1, 2, 3, 0, time.UTC)
	createdAtLTE := createdAtGTE.Add(24 * time.Hour)
	cursorCreatedAt := createdAtGTE.Add(12 * time.Hour)
	query, arguments := agentPageQuery(agentPageFilter{
		WorkspaceID:     42,
		Name:            "managed",
		Limit:           5,
		Cursor:          &AgentPageCursor{CreatedAt: cursorCreatedAt, ID: 17},
		IncludeArchived: true,
		CreatedAtGTE:    &createdAtGTE,
		CreatedAtLTE:    &createdAtLTE,
	})

	boundQuery, values, err := bindNamed(postgresRebinder{}, query, arguments)
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	if strings.Contains(boundQuery, ":") {
		t.Fatalf("bound query still contains named placeholders: %q", boundQuery)
	}
	wantValues := []any{
		int64(42),
		createdAtGTE,
		createdAtLTE,
		"managed",
		cursorCreatedAt,
		cursorCreatedAt,
		int64(17),
		6,
	}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("bindNamed() values = %#v, want %#v", values, wantValues)
	}
}

func TestAgentMutationQueryBindsJSONArguments(t *testing.T) {
	createdAt := time.Date(2026, time.July, 23, 4, 5, 6, 0, time.UTC)
	agent := Agent{
		UUID:              "4b01277d-4904-43c6-8d6a-3d866637d540",
		ExternalID:        "agent_sqlx",
		WorkspaceID:       11,
		CreatedByAPIKeyID: 12,
		Name:              "SQLX agent",
		Model:             json.RawMessage(`{"id":"claude-opus-4-6"}`),
		MCPServers:        json.RawMessage(`[]`),
		Metadata:          json.RawMessage(`{"source":"test"}`),
		Skills:            json.RawMessage(`[]`),
		Tools:             json.RawMessage(`[]`),
		CreatedAt:         createdAt,
	}

	boundQuery, values, err := bindNamed(postgresRebinder{}, createAgentSQL, agentArguments(agent))
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	if strings.Contains(boundQuery, ":") {
		t.Fatalf("bound query still contains named placeholders: %q", boundQuery)
	}
	if !strings.Contains(boundQuery, "CAST($8 AS jsonb)") {
		t.Fatalf("bound query JSON cast = %q, want model bound as PostgreSQL parameter $8", boundQuery)
	}
	if len(values) != 15 {
		t.Fatalf("bindNamed() value count = %d, want 15", len(values))
	}
	if model, ok := values[7].([]byte); !ok || !bytes.Equal(model, agent.Model) {
		t.Fatalf("bound model = %#v, want %s", values[7], agent.Model)
	}
	if values[10] != nil {
		t.Fatalf("bound multiagent = %#v, want nil", values[10])
	}
}

func TestAgentRowConversionCopiesJSON(t *testing.T) {
	row := agentRow{
		ID:             7,
		UUID:           "db-uuid",
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
	if agent.ID != row.ID || agent.ExternalID != row.ExternalID || agent.CurrentVersion != row.CurrentVersion {
		t.Fatalf("agent identity fields = %+v, want values from row %+v", agent, row)
	}
}

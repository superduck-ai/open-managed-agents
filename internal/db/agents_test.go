package db

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAgentPageQueryBindsNamedParameters(t *testing.T) {
	createdAtGTE := time.Date(2026, time.July, 20, 1, 2, 3, 0, time.UTC)
	createdAtLTE := createdAtGTE.Add(24 * time.Hour)
	cursorCreatedAt := createdAtGTE.Add(12 * time.Hour)
	query, arguments := agentPageQuery(agentPageFilter{
		WorkspaceUUID:   "00000000-0000-0000-0000-000000000042",
		Name:            "managed",
		Limit:           5,
		Cursor:          &AgentPageCursor{CreatedAt: cursorCreatedAt, UUID: "00000000-0000-0000-0000-000000000017"},
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
		uuid.MustParse("00000000-0000-0000-0000-000000000042"),
		createdAtGTE,
		createdAtLTE,
		"managed",
		cursorCreatedAt,
		cursorCreatedAt,
		uuid.MustParse("00000000-0000-0000-0000-000000000017"),
		6,
	}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("bindNamed() values = %#v, want %#v", values, wantValues)
	}
	if strings.Contains(boundQuery, " AS uuid)") {
		t.Fatalf("bound query contains UUID cast ceremony: %q", boundQuery)
	}
}

func TestAgentMutationQueryBindsJSONArguments(t *testing.T) {
	createdAt := time.Date(2026, time.July, 23, 4, 5, 6, 0, time.UTC)
	agent := Agent{
		UUID:                "4b01277d-4904-43c6-8d6a-3d866637d540",
		ExternalID:          "agent_sqlx",
		WorkspaceUUID:       "00000000-0000-0000-0000-000000000011",
		CreatedByAPIKeyUUID: "00000000-0000-0000-0000-000000000012",
		Name:                "SQLX agent",
		Model:               json.RawMessage(`{"id":"claude-opus-4-6"}`),
		MCPServers:          json.RawMessage(`[]`),
		Metadata:            json.RawMessage(`{"source":"test"}`),
		Skills:              json.RawMessage(`[]`),
		Tools:               json.RawMessage(`[]`),
		CreatedAt:           createdAt,
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
		UUID:           uuid.MustParse("00000000-0000-0000-0000-000000000007"),
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
	if agent.UUID != row.UUID.String() || agent.ExternalID != row.ExternalID || agent.CurrentVersion != row.CurrentVersion {
		t.Fatalf("agent identity fields = %+v, want values from row %+v", agent, row)
	}
}

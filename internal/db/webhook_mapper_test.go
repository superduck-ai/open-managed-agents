package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type webhookMapperContract struct {
	name      string
	statement yourbatis.Statement
	bound     yourbatis.BoundSQL
	id        string
	kind      yourbatis.StatementKind
	values    []any
	fragments []string
}

func TestWebhookMapperStatements(t *testing.T) {
	const (
		organizationUUID = "00000000-0000-4000-8000-000000000001"
		workspaceUUID    = "00000000-0000-4000-8000-000000000002"
		apiKeyUUID       = "00000000-0000-4000-8000-000000000003"
		endpointUUID     = "00000000-0000-4000-8000-000000000004"
	)
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	disabledReason := "temporary failure"
	events := json.RawMessage(`["session.status_idled"]`)
	insertParams := insertWebhookEndpointParams{
		UUID:                endpointUUID,
		ExternalID:          "wh_test",
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		CreatedByAPIKeyUUID: nullableString(apiKeyUUID),
		URL:                 "https://example.test/webhook",
		Name:                "Test webhook",
		Description:         "Test",
		EnabledEvents:       events,
		SigningSecret:       "secret",
		Status:              "enabled",
		DisabledReason:      &disabledReason,
		ConsecutiveFailures: 1,
		CreatedAt:           now,
	}
	updateParams := updateWebhookEndpointParams{
		WorkspaceUUID: workspaceUUID,
		ExternalID:    insertParams.ExternalID,
		URL:           nullableString("https://example.test/updated"),
		Name:          nullableString("Updated"),
		Description:   nullableString("Updated"),
		EnabledEvents: events,
		Status:        nullableString("enabled"),
		UpdatedAt:     now,
	}
	secretParams := regenerateWebhookEndpointSecretParams{
		WorkspaceUUID: workspaceUUID,
		ExternalID:    insertParams.ExternalID,
		SigningSecret: "new-secret",
		UpdatedAt:     now,
	}
	failEndpointParams := recordWebhookEndpointFailureParams{
		EndpointUUID: endpointUUID, WorkspaceUUID: workspaceUUID, DisableAfterMicroseconds: (24 * time.Hour).Microseconds(), Reason: disabledReason,
	}

	tests := []webhookMapperContract{
		{
			name: "workspace identifiers", statement: webhookWorkspaceMapperFindIdentifiersStatement,
			bound: buildWebhookWorkspaceMapperFindIdentifiers(yourbatis.DialectPostgres, workspaceUUID),
			id:    "WebhookWorkspaceMapper.FindIdentifiers", kind: yourbatis.StatementSelect,
			values: []any{workspaceUUID}, fragments: []string{"FROM workspaces", "uuid = $1"},
		},
		{
			name: "insert endpoint", statement: webhookEndpointMapperInsertStatement,
			bound: buildWebhookEndpointMapperInsert(yourbatis.DialectPostgres, insertParams),
			id:    "WebhookEndpointMapper.Insert", kind: yourbatis.StatementInsert,
			values: []any{
				endpointUUID, "wh_test", organizationUUID, workspaceUUID, insertParams.CreatedByAPIKeyUUID,
				insertParams.URL, insertParams.Name, insertParams.Description, events,
				insertParams.SigningSecret, insertParams.Status, &disabledReason,
				1, now, now,
			},
			fragments: []string{"INSERT INTO webhook_endpoints", "CAST($9 AS jsonb)", "RETURNING"},
		},
		{
			name: "list endpoints", statement: webhookEndpointMapperListStatement,
			bound: buildWebhookEndpointMapperList(yourbatis.DialectPostgres, workspaceUUID),
			id:    "WebhookEndpointMapper.List", kind: yourbatis.StatementSelect,
			values: []any{workspaceUUID}, fragments: []string{"FROM webhook_endpoints", "deleted_at IS NULL", "ORDER BY created_at DESC"},
		},
		{
			name: "find endpoint", statement: webhookEndpointMapperFindByExternalIDStatement,
			bound: buildWebhookEndpointMapperFindByExternalID(yourbatis.DialectPostgres, workspaceUUID, "wh_test"),
			id:    "WebhookEndpointMapper.FindByExternalID", kind: yourbatis.StatementSelect,
			values: []any{workspaceUUID, "wh_test"}, fragments: []string{"workspace_uuid = $1", "external_id = $2"},
		},
		{
			name: "update endpoint", statement: webhookEndpointMapperUpdateByExternalIDStatement,
			bound: buildWebhookEndpointMapperUpdateByExternalID(yourbatis.DialectPostgres, updateParams),
			id:    "WebhookEndpointMapper.UpdateByExternalID", kind: yourbatis.StatementUpdate,
			values: []any{
				updateParams.URL, updateParams.Name, updateParams.Description, events,
				updateParams.Status, updateParams.Status, updateParams.Status, updateParams.Status, now, workspaceUUID, "wh_test",
			},
			fragments: []string{"UPDATE webhook_endpoints", "CAST($4 AS jsonb)", "workspace_uuid = $10", "RETURNING"},
		},
		{
			name: "update signing secret", statement: webhookEndpointMapperUpdateSigningSecretStatement,
			bound: buildWebhookEndpointMapperUpdateSigningSecret(yourbatis.DialectPostgres, secretParams),
			id:    "WebhookEndpointMapper.UpdateSigningSecret", kind: yourbatis.StatementUpdate,
			values: []any{"new-secret", now, workspaceUUID, "wh_test"}, fragments: []string{"signing_secret = $1", "workspace_uuid = $3"},
		},
		{
			name: "soft delete endpoint", statement: webhookEndpointMapperSoftDeleteByExternalIDStatement,
			bound: buildWebhookEndpointMapperSoftDeleteByExternalID(yourbatis.DialectPostgres, workspaceUUID, "wh_test"),
			id:    "WebhookEndpointMapper.SoftDeleteByExternalID", kind: yourbatis.StatementUpdate,
			values: []any{workspaceUUID, "wh_test"}, fragments: []string{"deleted_at = NOW()", "workspace_uuid = $1", "external_id = $2"},
		},
		{
			name: "list active endpoints", statement: webhookEndpointMapperListActiveForEventStatement,
			bound: buildWebhookEndpointMapperListActiveForEvent(yourbatis.DialectPostgres, workspaceUUID, "session.status_idled"),
			id:    "WebhookEndpointMapper.ListActiveForEvent", kind: yourbatis.StatementSelect,
			values: []any{workspaceUUID, "session.status_idled"}, fragments: []string{"status = 'enabled'", "jsonb_exists(enabled_events, $2)", "ORDER BY created_at ASC"},
		},
		{
			name: "record delivery success", statement: webhookEndpointMapperRecordDeliverySuccessStatement,
			bound: buildWebhookEndpointMapperRecordDeliverySuccess(yourbatis.DialectPostgres, endpointUUID, workspaceUUID),
			id:    "WebhookEndpointMapper.RecordDeliverySuccess", kind: yourbatis.StatementUpdate,
			values: []any{endpointUUID, workspaceUUID}, fragments: []string{"consecutive_failures = 0", "uuid = $1", "workspace_uuid = $2"},
		},
		{
			name: "record delivery failure", statement: webhookEndpointMapperRecordDeliveryFailureStatement,
			bound: buildWebhookEndpointMapperRecordDeliveryFailure(yourbatis.DialectPostgres, failEndpointParams),
			id:    "WebhookEndpointMapper.RecordDeliveryFailure", kind: yourbatis.StatementUpdate,
			values:    []any{endpointUUID, workspaceUUID, false, (24 * time.Hour).Microseconds(), false, disabledReason, workspaceUUID},
			fragments: []string{"FOR UPDATE", "clock_timestamp() >= failure_started_at + $4", "THEN $6", "uuid = $1", "workspace_uuid = $2", "RETURNING e.status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertWebhookMapperContract(t, test)
		})
	}
}

func assertWebhookMapperContract(t *testing.T, contract webhookMapperContract) {
	t.Helper()
	if contract.statement.ID != contract.id || contract.statement.Kind != contract.kind || contract.statement.Source == "" {
		t.Fatalf("statement = %+v, want ID %q, kind %q, and source", contract.statement, contract.id, contract.kind)
	}
	if values := contract.bound.Values(); !reflect.DeepEqual(values, contract.values) {
		t.Fatalf("values = %#v, want %#v", values, contract.values)
	}
	if strings.Contains(contract.bound.SQL, "#{") || strings.Contains(contract.bound.SQL, "::") {
		t.Fatalf("SQL retains unsupported syntax: %q", contract.bound.SQL)
	}
	for _, fragment := range contract.fragments {
		if !strings.Contains(contract.bound.SQL, fragment) {
			t.Fatalf("SQL = %q, want fragment %q", contract.bound.SQL, fragment)
		}
	}
	for _, argument := range contract.bound.Args {
		wantSensitive := argument.Name == "payload" || argument.Name == "params.URL" ||
			argument.Name == "params.SigningSecret" || argument.Name == "params.DisabledReason" ||
			argument.Name == "params.Reason"
		if argument.Sensitive != wantSensitive {
			t.Fatalf("argument %q sensitive = %t, want %t", argument.Name, argument.Sensitive, wantSensitive)
		}
	}
}

func TestWebhookMapperResultSemantics(t *testing.T) {
	t.Run("workspace identifiers scan string UUID", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"organization_uuid", "workspace_external_id"},
			rows:    [][]driver.Value{{"00000000-0000-4000-8000-000000000001", "workspace_test"}},
		})
		row, err := NewWebhookWorkspaceMapper(executor).FindIdentifiers(context.Background(), "workspace")
		if err != nil || row.OrganizationUUID != "00000000-0000-4000-8000-000000000001" {
			t.Fatalf("FindIdentifiers() = (%+v, %v)", row, err)
		}
	})

	t.Run("single row zero result", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: webhookEndpointMapperTestColumns()})
		_, err := NewWebhookEndpointMapper(executor).FindByExternalID(context.Background(), "workspace", "wh_test")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("endpoint row and nullable values", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: webhookEndpointMapperTestColumns(),
			rows:    [][]driver.Value{webhookEndpointMapperTestRow()},
		})
		row, err := NewWebhookEndpointMapper(executor).Insert(context.Background(), insertWebhookEndpointParams{})
		endpoint, mapErr := row.endpoint()
		if err != nil || mapErr != nil || endpoint.UUID != "00000000-0000-4000-8000-000000000004" || endpoint.DisabledReason == nil {
			t.Fatalf("Insert() = (%+v, %v, %v)", endpoint, err, mapErr)
		}
	})

	t.Run("many rows empty and populated", func(t *testing.T) {
		emptyExecutor := newMapperTestExecutor(t, mapperTestResponse{columns: webhookEndpointMapperTestColumns()})
		rows, err := NewWebhookEndpointMapper(emptyExecutor).List(context.Background(), "workspace")
		if err != nil || len(rows) != 0 {
			t.Fatalf("List() = (%+v, %v), want empty result", rows, err)
		}
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: webhookEndpointMapperTestColumns(),
			rows:    [][]driver.Value{webhookEndpointMapperTestRow(), webhookEndpointMapperTestRow()},
		})
		rows, err = NewWebhookEndpointMapper(executor).List(context.Background(), "workspace")
		if err != nil || len(rows) != 2 {
			t.Fatalf("List() = (%+v, %v)", rows, err)
		}
	})

	t.Run("rows affected", func(t *testing.T) {
		rowsExecutor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		rowsAffected, err := NewWebhookEndpointMapper(rowsExecutor).UpdateSigningSecret(context.Background(), regenerateWebhookEndpointSecretParams{})
		if err != nil || rowsAffected != 1 {
			t.Fatalf("UpdateSigningSecret() = (%d, %v)", rowsAffected, err)
		}
	})
}

func webhookEndpointMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "created_by_api_key_uuid",
		"url", "name", "description", "enabled_events", "signing_secret", "status", "disabled_reason",
		"consecutive_failures", "failure_started_at", "created_at", "updated_at", "deleted_at",
	}
}

func webhookEndpointMapperTestRow() []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000004", "wh_test",
		"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003", "https://example.test", "Test", "Description",
		[]byte(`["session.status_idled"]`), "secret", "disabled", "temporary failure", 1, nil, now, now, nil,
	}
}

func TestWebhookEndpointPartialUpdateSQL(t *testing.T) {
	for _, name := range []string{"empty", "name", "description", "url", "events", "status"} {
		t.Run(name, func(t *testing.T) {
			value := ""
			params := updateWebhookEndpointParams{WorkspaceUUID: "workspace", ExternalID: "wh_test", UpdatedAt: time.Now()}
			switch name {
			case "name":
				params.Name = &value
			case "description":
				params.Description = &value
			case "url":
				params.URL = &value
			case "events":
				params.EnabledEvents = json.RawMessage(`[]`)
			case "status":
				value = "disabled"
				params.Status = &value
			}
			bound := buildWebhookEndpointMapperUpdateByExternalID(yourbatis.DialectPostgres, params)
			query := bound.SQL
			var values []any
			switch name {
			case "status":
				values = []any{params.Status, params.Status, params.Status, params.Status}
			case "events":
				values = []any{params.EnabledEvents}
			case "name", "description", "url":
				values = []any{&value}
			}
			values = append(values, params.UpdatedAt, params.WorkspaceUUID, params.ExternalID)
			if !reflect.DeepEqual(bound.Values(), values) {
				t.Fatalf("bindings = %#v, want %#v", bound.Values(), values)
			}
			for _, arg := range bound.Args {
				if arg.Sensitive != (arg.Name == "params.URL") {
					t.Fatalf("sensitive binding = %+v", arg)
				}
			}
			for _, field := range []string{"name", "description", "url", "status"} {
				if strings.Contains(query, field+" =") != (field == name) {
					t.Fatalf("%s patch writes %s: %s", name, field, query)
				}
			}
			if strings.Contains(query, "consecutive_failures =") != (name == "status") {
				t.Fatalf("stats overwritten: %s", query)
			}
			if !strings.Contains(query, "deleted_at IS NULL") || !strings.Contains(query, "workspace_uuid =") {
				t.Fatal(query)
			}
		})
	}
}

func TestWebhookDeliveryTargetMapper(t *testing.T) {
	bound := buildWebhookEndpointMapperFindDeliveryTarget(yourbatis.DialectPostgres, "workspace", "endpoint")
	assertWebhookMapperContract(t, webhookMapperContract{
		name: "delivery target", statement: webhookEndpointMapperFindDeliveryTargetStatement, bound: bound,
		id: "WebhookEndpointMapper.FindDeliveryTarget", kind: yourbatis.StatementSelect,
		values: []any{"workspace", "endpoint"}, fragments: []string{"SELECT url, signing_secret, status", "workspace_uuid = $1", "uuid = $2", "deleted_at IS NULL"},
	})
	for _, populated := range []bool{false, true} {
		var rows [][]driver.Value
		if populated {
			rows = [][]driver.Value{{"https://example.com", "secret", "enabled"}}
		}
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"url", "signing_secret", "status"}, rows: rows})
		target, found, err := NewWebhookEndpointMapper(executor).FindDeliveryTarget(t.Context(), "workspace", "endpoint")
		if err != nil || found != populated || (found && target.Status != "enabled") {
			t.Fatalf("target=%+v %t %v", target, found, err)
		}
	}
	ids := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}, rows: [][]driver.Value{{"one"}, {"two"}}})
	rows, err := NewWebhookEndpointMapper(ids).ListActiveForEvent(t.Context(), "workspace", "event")
	if err != nil || len(rows) != 2 || rows[1].UUID != "two" {
		t.Fatalf("ids=%+v %v", rows, err)
	}
	assertMapperExecutionError(t, mapperExecutionErrorContract{"WebhookEndpointMapper.FindDeliveryTarget", yourbatis.StatementSelect, true, func(executor yourbatis.Executor) error {
		_, _, err := NewWebhookEndpointMapper(executor).FindDeliveryTarget(t.Context(), "workspace", "endpoint")
		return err
	}})
}

func TestWebhookMapperMethodsPropagateExecutionErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{"workspace identifiers", mapperExecutionErrorContract{"WebhookWorkspaceMapper.FindIdentifiers", yourbatis.StatementSelect, true, func(executor yourbatis.Executor) error {
			_, err := NewWebhookWorkspaceMapper(executor).FindIdentifiers(ctx, "workspace")
			return err
		}}},
		{"insert endpoint", mapperExecutionErrorContract{"WebhookEndpointMapper.Insert", yourbatis.StatementInsert, true, func(executor yourbatis.Executor) error {
			_, err := NewWebhookEndpointMapper(executor).Insert(ctx, insertWebhookEndpointParams{})
			return err
		}}},
		{"list endpoints", mapperExecutionErrorContract{"WebhookEndpointMapper.List", yourbatis.StatementSelect, true, func(executor yourbatis.Executor) error {
			_, err := NewWebhookEndpointMapper(executor).List(ctx, "workspace")
			return err
		}}},
		{"find endpoint", mapperExecutionErrorContract{"WebhookEndpointMapper.FindByExternalID", yourbatis.StatementSelect, true, func(executor yourbatis.Executor) error {
			_, err := NewWebhookEndpointMapper(executor).FindByExternalID(ctx, "workspace", "external")
			return err
		}}},
		{"update endpoint", mapperExecutionErrorContract{"WebhookEndpointMapper.UpdateByExternalID", yourbatis.StatementUpdate, true, func(executor yourbatis.Executor) error {
			_, err := NewWebhookEndpointMapper(executor).UpdateByExternalID(ctx, updateWebhookEndpointParams{})
			return err
		}}},
		{"update secret", mapperExecutionErrorContract{"WebhookEndpointMapper.UpdateSigningSecret", yourbatis.StatementUpdate, false, func(executor yourbatis.Executor) error {
			_, err := NewWebhookEndpointMapper(executor).UpdateSigningSecret(ctx, regenerateWebhookEndpointSecretParams{})
			return err
		}}},
		{"delete endpoint", mapperExecutionErrorContract{"WebhookEndpointMapper.SoftDeleteByExternalID", yourbatis.StatementUpdate, false, func(executor yourbatis.Executor) error {
			_, err := NewWebhookEndpointMapper(executor).SoftDeleteByExternalID(ctx, "workspace", "external")
			return err
		}}},
		{"list active endpoints", mapperExecutionErrorContract{"WebhookEndpointMapper.ListActiveForEvent", yourbatis.StatementSelect, true, func(executor yourbatis.Executor) error {
			_, err := NewWebhookEndpointMapper(executor).ListActiveForEvent(ctx, "workspace", "event")
			return err
		}}},
		{"record success", mapperExecutionErrorContract{"WebhookEndpointMapper.RecordDeliverySuccess", yourbatis.StatementUpdate, false, func(executor yourbatis.Executor) error {
			return NewWebhookEndpointMapper(executor).RecordDeliverySuccess(ctx, "endpoint", "workspace")
		}}},
		{"record failure", mapperExecutionErrorContract{"WebhookEndpointMapper.RecordDeliveryFailure", yourbatis.StatementUpdate, true, func(executor yourbatis.Executor) error {
			_, err := NewWebhookEndpointMapper(executor).RecordDeliveryFailure(ctx, recordWebhookEndpointFailureParams{})
			return err
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperExecutionError(t, test.contract)
		})
	}
}

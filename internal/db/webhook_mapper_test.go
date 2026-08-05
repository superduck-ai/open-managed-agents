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
		jobUUID          = "00000000-0000-4000-8000-000000000005"
	)
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	disabledReason := "temporary failure"
	payload := []byte(`{"event_type":"session.created","event":{"type":"session.created"}}`)
	events := json.RawMessage(`["session.created"]`)
	insertParams := insertWebhookEndpointParams{
		UUID:                endpointUUID,
		ExternalID:          "wh_test",
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		CreatedByAPIKeyUUID: apiKeyUUID,
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
		WorkspaceUUID:       workspaceUUID,
		ExternalID:          insertParams.ExternalID,
		URL:                 "https://example.test/updated",
		Name:                "Updated",
		Description:         "Updated",
		EnabledEvents:       events,
		Status:              "enabled",
		DisabledReason:      &disabledReason,
		ConsecutiveFailures: 2,
		UpdatedAt:           now,
	}
	secretParams := regenerateWebhookEndpointSecretParams{
		WorkspaceUUID: workspaceUUID,
		ExternalID:    insertParams.ExternalID,
		SigningSecret: "new-secret",
		UpdatedAt:     now,
	}
	failJobParams := failWebhookDeliveryJobParams{
		JobUUID: jobUUID, Status: "retry", RunAfter: now, Attempts: 2, Reason: disabledReason,
	}
	failEndpointParams := recordWebhookEndpointFailureParams{
		EndpointUUID: endpointUUID, DisableAfter: 20, Reason: disabledReason,
	}

	tests := []webhookMapperContract{
		{
			name: "workspace identifiers", statement: webhookWorkspaceMapperFindIdentifiersStatement,
			bound: buildWebhookWorkspaceMapperFindIdentifiers(yourbatis.DialectPostgres, workspaceUUID),
			id:    "WebhookWorkspaceMapper.FindIdentifiers", kind: yourbatis.StatementSelect,
			values: []any{workspaceUUID}, fragments: []string{"FROM workspaces", "uuid = $1"},
		},
		{
			name: "insert delivery job", statement: webhookDeliveryJobMapperInsertStatement,
			bound: buildWebhookDeliveryJobMapperInsert(yourbatis.DialectPostgres, workspaceUUID, payload),
			id:    "WebhookDeliveryJobMapper.Insert", kind: yourbatis.StatementInsert,
			values: []any{workspaceUUID, payload}, fragments: []string{"INSERT INTO jobs", "'webhook_delivery'", "CAST($2 AS jsonb)"},
		},
		{
			name: "lease delivery jobs", statement: webhookDeliveryJobMapperLeaseStatement,
			bound: buildWebhookDeliveryJobMapperLease(yourbatis.DialectPostgres, "worker_test", 10, time.Minute.Microseconds()),
			id:    "WebhookDeliveryJobMapper.Lease", kind: yourbatis.StatementSelect,
			values:    []any{10, "worker_test", time.Minute.Microseconds()},
			fragments: []string{"FOR UPDATE SKIP LOCKED", "UPDATE jobs", "LEFT JOIN webhook_endpoints", "AS uuid)"},
		},
		{
			name: "complete delivery job", statement: webhookDeliveryJobMapperCompleteStatement,
			bound: buildWebhookDeliveryJobMapperComplete(yourbatis.DialectPostgres, jobUUID),
			id:    "WebhookDeliveryJobMapper.Complete", kind: yourbatis.StatementUpdate,
			values: []any{jobUUID}, fragments: []string{"UPDATE jobs", "status = 'completed'", "uuid = $1"},
		},
		{
			name: "fail delivery job", statement: webhookDeliveryJobMapperFailStatement,
			bound: buildWebhookDeliveryJobMapperFail(yourbatis.DialectPostgres, failJobParams),
			id:    "WebhookDeliveryJobMapper.Fail", kind: yourbatis.StatementUpdate,
			values:    []any{"retry", now, 2, disabledReason, jobUUID},
			fragments: []string{"UPDATE jobs", "jsonb_build_object", "CAST($4 AS text)", "uuid = $5"},
		},
		{
			name: "insert endpoint", statement: webhookEndpointMapperInsertStatement,
			bound: buildWebhookEndpointMapperInsert(yourbatis.DialectPostgres, insertParams),
			id:    "WebhookEndpointMapper.Insert", kind: yourbatis.StatementInsert,
			values: []any{
				endpointUUID, "wh_test", organizationUUID, workspaceUUID, apiKeyUUID,
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
				updateParams.Status, &disabledReason, 2, now, workspaceUUID, "wh_test",
			},
			fragments: []string{"UPDATE webhook_endpoints", "CAST($4 AS jsonb)", "workspace_uuid = $9", "RETURNING"},
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
			name: "endpoint exists", statement: webhookEndpointMapperExistsStatement,
			bound: buildWebhookEndpointMapperExists(yourbatis.DialectPostgres, workspaceUUID),
			id:    "WebhookEndpointMapper.Exists", kind: yourbatis.StatementSelect,
			values: []any{workspaceUUID}, fragments: []string{"SELECT EXISTS", "workspace_uuid = $1"},
		},
		{
			name: "list active endpoints", statement: webhookEndpointMapperListActiveForEventStatement,
			bound: buildWebhookEndpointMapperListActiveForEvent(yourbatis.DialectPostgres, workspaceUUID, "session.created"),
			id:    "WebhookEndpointMapper.ListActiveForEvent", kind: yourbatis.StatementSelect,
			values: []any{workspaceUUID, "session.created"}, fragments: []string{"status = 'enabled'", "jsonb_exists(enabled_events, $2)", "ORDER BY created_at ASC"},
		},
		{
			name: "record delivery success", statement: webhookEndpointMapperRecordDeliverySuccessStatement,
			bound: buildWebhookEndpointMapperRecordDeliverySuccess(yourbatis.DialectPostgres, endpointUUID),
			id:    "WebhookEndpointMapper.RecordDeliverySuccess", kind: yourbatis.StatementUpdate,
			values: []any{endpointUUID}, fragments: []string{"consecutive_failures = 0", "uuid = $1"},
		},
		{
			name: "record delivery failure", statement: webhookEndpointMapperRecordDeliveryFailureStatement,
			bound: buildWebhookEndpointMapperRecordDeliveryFailure(yourbatis.DialectPostgres, failEndpointParams),
			id:    "WebhookEndpointMapper.RecordDeliveryFailure", kind: yourbatis.StatementUpdate,
			values:    []any{20, 20, disabledReason, endpointUUID},
			fragments: []string{"consecutive_failures + 1 >= $1", "THEN $3", "uuid = $4"},
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
		rows, err = NewWebhookEndpointMapper(executor).ListActiveForEvent(context.Background(), "workspace", "event")
		if err != nil || len(rows) != 2 {
			t.Fatalf("ListActiveForEvent() = (%+v, %v)", rows, err)
		}
	})

	t.Run("lease row scans nullable string UUID", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: webhookDeliveryJobMapperTestColumns(),
			rows:    [][]driver.Value{webhookDeliveryJobMapperTestRow()},
		})
		rows, err := NewWebhookDeliveryJobMapper(executor).Lease(context.Background(), "worker", 1, 1)
		if err != nil || len(rows) != 1 {
			t.Fatalf("Lease() = (%+v, %v)", rows, err)
		}
		job := rows[0].job()
		if job.WebhookEndpointUUID == nil || *job.WebhookEndpointUUID != "00000000-0000-4000-8000-000000000004" {
			t.Fatalf("Lease() job = %+v", job)
		}
	})

	t.Run("scalar and rows affected", func(t *testing.T) {
		existsExecutor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"exists"}, rows: [][]driver.Value{{true}},
		})
		exists, err := NewWebhookEndpointMapper(existsExecutor).Exists(context.Background(), "workspace")
		if err != nil || !exists {
			t.Fatalf("Exists() = (%t, %v)", exists, err)
		}
		rowsExecutor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		rowsAffected, err := NewWebhookEndpointMapper(rowsExecutor).UpdateSigningSecret(context.Background(), regenerateWebhookEndpointSecretParams{})
		if err != nil || rowsAffected != 1 {
			t.Fatalf("UpdateSigningSecret() = (%d, %v)", rowsAffected, err)
		}
	})
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
		{"insert job", mapperExecutionErrorContract{"WebhookDeliveryJobMapper.Insert", yourbatis.StatementInsert, false, func(executor yourbatis.Executor) error {
			return NewWebhookDeliveryJobMapper(executor).Insert(ctx, "workspace", nil)
		}}},
		{"lease jobs", mapperExecutionErrorContract{"WebhookDeliveryJobMapper.Lease", yourbatis.StatementSelect, true, func(executor yourbatis.Executor) error {
			_, err := NewWebhookDeliveryJobMapper(executor).Lease(ctx, "worker", 1, 1)
			return err
		}}},
		{"complete job", mapperExecutionErrorContract{"WebhookDeliveryJobMapper.Complete", yourbatis.StatementUpdate, false, func(executor yourbatis.Executor) error {
			return NewWebhookDeliveryJobMapper(executor).Complete(ctx, "job")
		}}},
		{"fail job", mapperExecutionErrorContract{"WebhookDeliveryJobMapper.Fail", yourbatis.StatementUpdate, false, func(executor yourbatis.Executor) error {
			return NewWebhookDeliveryJobMapper(executor).Fail(ctx, failWebhookDeliveryJobParams{})
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
		{"endpoint exists", mapperExecutionErrorContract{"WebhookEndpointMapper.Exists", yourbatis.StatementSelect, true, func(executor yourbatis.Executor) error {
			_, err := NewWebhookEndpointMapper(executor).Exists(ctx, "workspace")
			return err
		}}},
		{"list active endpoints", mapperExecutionErrorContract{"WebhookEndpointMapper.ListActiveForEvent", yourbatis.StatementSelect, true, func(executor yourbatis.Executor) error {
			_, err := NewWebhookEndpointMapper(executor).ListActiveForEvent(ctx, "workspace", "event")
			return err
		}}},
		{"record success", mapperExecutionErrorContract{"WebhookEndpointMapper.RecordDeliverySuccess", yourbatis.StatementUpdate, false, func(executor yourbatis.Executor) error {
			return NewWebhookEndpointMapper(executor).RecordDeliverySuccess(ctx, "endpoint")
		}}},
		{"record failure", mapperExecutionErrorContract{"WebhookEndpointMapper.RecordDeliveryFailure", yourbatis.StatementUpdate, false, func(executor yourbatis.Executor) error {
			return NewWebhookEndpointMapper(executor).RecordDeliveryFailure(ctx, recordWebhookEndpointFailureParams{})
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperExecutionError(t, test.contract)
		})
	}
}

func webhookEndpointMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "created_by_api_key_uuid",
		"url", "name", "description", "enabled_events", "signing_secret", "status", "disabled_reason",
		"consecutive_failures", "created_at", "updated_at", "deleted_at",
	}
}

func webhookEndpointMapperTestRow() []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000004", "wh_test",
		"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003", "https://example.test", "Test", "Description",
		[]byte(`["session.created"]`), "secret", "disabled", "temporary failure", 1, now, now, nil,
	}
}

func webhookDeliveryJobMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "workspace_uuid", "event_type", "event", "attempts",
		"webhook_endpoint_uuid", "webhook_endpoint_external_id", "webhook_endpoint_url",
		"webhook_endpoint_secret", "webhook_endpoint_status",
	}
}

func webhookDeliveryJobMapperTestRow() []driver.Value {
	return []driver.Value{
		"00000000-0000-4000-8000-000000000005", "job_test",
		"00000000-0000-4000-8000-000000000002", "session.created", []byte(`{"type":"session.created"}`), 1,
		"00000000-0000-4000-8000-000000000004", "wh_test", "https://example.test", "secret", "enabled",
	}
}

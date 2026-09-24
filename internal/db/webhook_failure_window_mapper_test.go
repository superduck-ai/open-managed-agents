package db

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestWebhookFailureWindowMapperResults(t *testing.T) {
	for _, populated := range []bool{false, true} {
		var rows [][]driver.Value
		if populated {
			rows = [][]driver.Value{{"job_uuid"}}
		}
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}, rows: rows})
		row, found, err := NewWebhookDeliveryJobMapper(executor).LockClaim(t.Context(), "job", "workspace", "claim")
		if err != nil || found != populated || (found && row.UUID != "job_uuid") {
			t.Fatalf("claim=%+v %t %v", row, found, err)
		}
	}
	for _, state := range []string{"missing", "enabled", "disabled"} {
		var rows [][]driver.Value
		if state != "missing" {
			rows = [][]driver.Value{{state == "disabled"}}
		}
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"disabled"}, rows: rows})
		row, err := NewWebhookEndpointMapper(executor).RecordDeliveryFailure(t.Context(), recordWebhookEndpointFailureParams{})
		if state == "missing" {
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("missing=%v", err)
			}
			continue
		}
		if err != nil || row.Disabled != (state == "disabled") {
			t.Fatalf("result=%+v %v", row, err)
		}
	}
	for _, started := range []*time.Time{nil, new(time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC))} {
		values := webhookEndpointMapperTestRow()
		if started != nil {
			values[13] = *started
		}
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: webhookEndpointMapperTestColumns(), rows: [][]driver.Value{values}})
		row, err := NewWebhookEndpointMapper(executor).FindByExternalID(t.Context(), "workspace", "wh_test")
		endpoint, mapErr := row.endpoint()
		if err != nil || mapErr != nil || (endpoint.FailureStartedAt == nil) != (started == nil) {
			t.Fatalf("scan=%+v %v %v", endpoint, err, mapErr)
		}
		if started != nil && !endpoint.FailureStartedAt.Equal(*started) {
			t.Fatal("lost timestamp")
		}
	}
}

func TestWebhookFailureWindowClaimSQL(t *testing.T) {
	bound := buildWebhookDeliveryJobMapperLockClaim(yourbatis.DialectPostgres, "job", "workspace", "claim")
	assertWebhookMapperContract(t, webhookMapperContract{
		name: "lock claim", statement: webhookDeliveryJobMapperLockClaimStatement, bound: bound,
		id: "WebhookDeliveryJobMapper.LockClaim", kind: yourbatis.StatementSelect,
		values:    []any{"job", "workspace", "claim"},
		fragments: []string{"FOR UPDATE", "uuid = $1", "workspace_uuid = $2", "locked_by = $3", "status = 'running'", "locked_until > clock_timestamp()", "type = 'webhook_delivery'"},
	})
	assertMapperExecutionError(t, mapperExecutionErrorContract{
		"WebhookDeliveryJobMapper.LockClaim", yourbatis.StatementSelect, true, func(executor yourbatis.Executor) error {
			_, _, err := NewWebhookDeliveryJobMapper(executor).LockClaim(t.Context(), "job", "workspace", "claim")
			return err
		},
	})
}

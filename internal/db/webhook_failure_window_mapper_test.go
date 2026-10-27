package db

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"
)

func TestWebhookFailureWindowMapperResults(t *testing.T) {
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

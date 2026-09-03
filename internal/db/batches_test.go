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

func TestMessageBatchMapperStatements(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	organizationUUID := "00000000-0000-4000-8000-000000000000"
	workspaceUUID := "00000000-0000-4000-8000-000000000001"
	batchUUID := "00000000-0000-4000-8000-000000000002"
	requestUUID := "00000000-0000-4000-8000-000000000003"
	jobUUID := "00000000-0000-4000-8000-000000000004"
	result := json.RawMessage(`{"type":"succeeded"}`)
	insert := insertMessageBatchParams{
		UUID:                batchUUID,
		ExternalID:          "msgbatch_mapper",
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		CreatedByAPIKeyUUID: nullableString("00000000-0000-4000-8000-000000000005"),
		APIVariant:          "stable",
		AnthropicVersion:    "2023-06-01",
		BetaHeaders:         json.RawMessage(`[]`),
		RequestCount:        2,
		CreatedAt:           now,
		ExpiresAt:           now.Add(time.Hour),
	}
	insertRequest := insertMessageBatchRequestParams{
		ExternalID:       "msgbatchreq_mapper",
		WorkspaceUUID:    workspaceUUID,
		MessageBatchUUID: batchUUID,
		RequestIndex:     1,
		CustomID:         "custom-id",
		Params:           json.RawMessage(`{"model":"claude-test"}`),
	}
	finalize := finalizeMessageBatchParams{
		BatchUUID:     batchUUID,
		Processing:    0,
		Succeeded:     1,
		Errored:       2,
		Canceled:      3,
		Expired:       4,
		ResultsBucket: "bucket",
		ResultsKey:    "key",
		ResultsSize:   42,
		ResultsSHA256: "sha256",
		EndedAt:       now,
	}
	completeRequest := completeMessageBatchRequestParams{
		RequestUUID:       requestUUID,
		Status:            "succeeded",
		Result:            result,
		UpstreamRequestID: "request-upstream",
		CompletedAt:       now,
	}
	failJob := failMessageBatchJobParams{
		JobUUID:  jobUUID,
		Status:   "retry",
		RunAfter: now.Add(time.Minute),
		Reason:   "upstream unavailable",
		Attempts: 2,
	}
	payload := []byte(`{"message_batch_uuid":"` + batchUUID + `"}`)

	tests := []struct {
		name      string
		statement yourbatis.Statement
		bound     yourbatis.BoundSQL
		id        string
		kind      yourbatis.StatementKind
		values    []any
		fragments []string
	}{
		{"insert batch", messageBatchMapperInsertStatement, buildMessageBatchMapperInsert(yourbatis.DialectPostgres, insert), "MessageBatchMapper.Insert", yourbatis.StatementInsert, []any{batchUUID, insert.ExternalID, organizationUUID, workspaceUUID, insert.CreatedByAPIKeyUUID, "stable", "2023-06-01", insert.BetaHeaders, 2, 2, now, insert.ExpiresAt}, []string{"INSERT INTO message_batches", "CAST($8 AS jsonb)", "RETURNING uuid, created_at, updated_at"}},
		{"insert request", messageBatchMapperInsertRequestStatement, buildMessageBatchMapperInsertRequest(yourbatis.DialectPostgres, insertRequest), "MessageBatchMapper.InsertRequest", yourbatis.StatementInsert, []any{insertRequest.ExternalID, workspaceUUID, batchUUID, 1, "custom-id", insertRequest.Params}, []string{"INSERT INTO message_batch_requests", "CAST($6 AS jsonb)"}},
		{"insert job", messageBatchMapperInsertJobStatement, buildMessageBatchMapperInsertJob(yourbatis.DialectPostgres, workspaceUUID, payload), "MessageBatchMapper.InsertJob", yourbatis.StatementInsert, []any{workspaceUUID, payload}, []string{"INSERT INTO jobs", "CAST($2 AS jsonb)"}},
		{"find by external ID", messageBatchMapperFindByExternalIDStatement, buildMessageBatchMapperFindByExternalID(yourbatis.DialectPostgres, workspaceUUID, insert.ExternalID), "MessageBatchMapper.FindByExternalID", yourbatis.StatementSelect, []any{workspaceUUID, insert.ExternalID}, []string{"workspace_uuid = $1", "external_id = $2", "deleted_at IS NULL"}},
		{"find by UUID", messageBatchMapperFindByUUIDStatement, buildMessageBatchMapperFindByUUID(yourbatis.DialectPostgres, batchUUID), "MessageBatchMapper.FindByUUID", yourbatis.StatementSelect, []any{batchUUID}, []string{"WHERE mb.uuid = $1"}},
		{"find page anchor", messageBatchMapperFindPageAnchorByExternalIDStatement, buildMessageBatchMapperFindPageAnchorByExternalID(yourbatis.DialectPostgres, workspaceUUID, insert.ExternalID), "MessageBatchMapper.FindPageAnchorByExternalID", yourbatis.StatementSelect, []any{workspaceUUID, insert.ExternalID}, []string{"SELECT uuid, created_at", "workspace_uuid = $1"}},
		{"list page", messageBatchMapperListPageStatement, buildMessageBatchMapperListPage(yourbatis.DialectPostgres, workspaceUUID, nil, false, 21), "MessageBatchMapper.ListPage", yourbatis.StatementSelect, []any{workspaceUUID, 21}, []string{"ORDER BY mb.created_at DESC, mb.uuid DESC", "LIMIT $2"}},
		{"mark canceling", messageBatchMapperMarkCancelingByExternalIDStatement, buildMessageBatchMapperMarkCancelingByExternalID(yourbatis.DialectPostgres, workspaceUUID, insert.ExternalID), "MessageBatchMapper.MarkCancelingByExternalID", yourbatis.StatementUpdate, []any{workspaceUUID, insert.ExternalID}, []string{"processing_status = 'canceling'", "workspace_uuid = $1"}},
		{"exists", messageBatchMapperExistsByExternalIDStatement, buildMessageBatchMapperExistsByExternalID(yourbatis.DialectPostgres, workspaceUUID, insert.ExternalID), "MessageBatchMapper.ExistsByExternalID", yourbatis.StatementSelect, []any{workspaceUUID, insert.ExternalID}, []string{"SELECT EXISTS", "workspace_uuid = $1"}},
		{"soft delete", messageBatchMapperSoftDeleteEndedByExternalIDStatement, buildMessageBatchMapperSoftDeleteEndedByExternalID(yourbatis.DialectPostgres, workspaceUUID, insert.ExternalID), "MessageBatchMapper.SoftDeleteEndedByExternalID", yourbatis.StatementUpdate, []any{workspaceUUID, insert.ExternalID}, []string{"SET deleted_at = NOW()", "processing_status = 'ended'"}},
		{"find processing status", messageBatchMapperFindProcessingStatusByExternalIDStatement, buildMessageBatchMapperFindProcessingStatusByExternalID(yourbatis.DialectPostgres, workspaceUUID, insert.ExternalID), "MessageBatchMapper.FindProcessingStatusByExternalID", yourbatis.StatementSelect, []any{workspaceUUID, insert.ExternalID}, []string{"SELECT processing_status", "workspace_uuid = $1"}},
		{"finalize", messageBatchMapperFinalizeStatement, buildMessageBatchMapperFinalize(yourbatis.DialectPostgres, finalize), "MessageBatchMapper.Finalize", yourbatis.StatementUpdate, []any{now, 0, 1, 2, 3, 4, "bucket", "key", int64(42), "sha256", batchUUID}, []string{"processing_status = 'ended'", "WHERE uuid = $11"}},
		{"finalize pending requests", messageBatchMapperFinalizePendingRequestsStatement, buildMessageBatchMapperFinalizePendingRequests(yourbatis.DialectPostgres, batchUUID, "canceled", result), "MessageBatchMapper.FinalizePendingRequests", yourbatis.StatementUpdate, []any{"canceled", result, batchUUID}, []string{"result = CAST($2 AS jsonb)", "message_batch_uuid = $3"}},
		{"mark stale requests", messageBatchMapperMarkStaleInFlightRequestsErroredStatement, buildMessageBatchMapperMarkStaleInFlightRequestsErrored(yourbatis.DialectPostgres, batchUUID, now, result), "MessageBatchMapper.MarkStaleInFlightRequestsErrored", yourbatis.StatementUpdate, []any{result, batchUUID, now}, []string{"status = 'errored'", "started_at < $3"}},
		{"count request statuses", messageBatchMapperCountRequestsByStatusStatement, buildMessageBatchMapperCountRequestsByStatus(yourbatis.DialectPostgres, batchUUID), "MessageBatchMapper.CountRequestsByStatus", yourbatis.StatementSelect, []any{batchUUID}, []string{"COUNT(*) FILTER", "message_batch_uuid = $1"}},
		{"list expired", messageBatchMapperListExpiredStatement, buildMessageBatchMapperListExpired(yourbatis.DialectPostgres, now, 100), "MessageBatchMapper.ListExpired", yourbatis.StatementSelect, []any{now, 100}, []string{"expires_at <= $1", "LIMIT $2"}},
		{"find request", messageBatchMapperFindRequestByIndexStatement, buildMessageBatchMapperFindRequestByIndex(yourbatis.DialectPostgres, batchUUID, 1), "MessageBatchMapper.FindRequestByIndex", yourbatis.StatementSelect, []any{batchUUID, 1}, []string{"message_batch_uuid = $1", "request_index = $2"}},
		{"list requests", messageBatchMapperListRequestsOrderedStatement, buildMessageBatchMapperListRequestsOrdered(yourbatis.DialectPostgres, batchUUID), "MessageBatchMapper.ListRequestsOrdered", yourbatis.StatementSelect, []any{batchUUID}, []string{"message_batch_uuid = $1", "ORDER BY request_index"}},
		{"claim request", messageBatchMapperClaimRequestStatement, buildMessageBatchMapperClaimRequest(yourbatis.DialectPostgres, requestUUID, "worker", now), "MessageBatchMapper.ClaimRequest", yourbatis.StatementUpdate, []any{now, "worker", requestUUID}, []string{"status = 'in_flight'", "WHERE uuid = $3"}},
		{"complete request", messageBatchMapperCompleteRequestStatement, buildMessageBatchMapperCompleteRequest(yourbatis.DialectPostgres, completeRequest), "MessageBatchMapper.CompleteRequest", yourbatis.StatementUpdate, []any{"succeeded", result, "request-upstream", now, requestUUID}, []string{"result = CAST($2 AS jsonb)", "WHERE uuid = $5"}},
		{"lease jobs", messageBatchMapperLeaseJobsStatement, buildMessageBatchMapperLeaseJobs(yourbatis.DialectPostgres, "worker", 5, 60_000_000), "MessageBatchMapper.LeaseJobs", yourbatis.StatementUpdate, []any{5, "worker", int64(60_000_000)}, []string{"FOR UPDATE SKIP LOCKED", "locked_by = $2", "$3 * INTERVAL '1 microsecond'", "RETURNING j.uuid"}},
		{"extend job lease", messageBatchMapperExtendJobLeaseStatement, buildMessageBatchMapperExtendJobLease(yourbatis.DialectPostgres, jobUUID, "worker", 60_000_000), "MessageBatchMapper.ExtendJobLease", yourbatis.StatementUpdate, []any{int64(60_000_000), jobUUID, "worker"}, []string{"locked_until = NOW() + $1", "WHERE uuid = $2"}},
		{"complete job", messageBatchMapperCompleteJobStatement, buildMessageBatchMapperCompleteJob(yourbatis.DialectPostgres, jobUUID), "MessageBatchMapper.CompleteJob", yourbatis.StatementUpdate, []any{jobUUID}, []string{"status = 'completed'", "WHERE uuid = $1"}},
		{"fail job", messageBatchMapperFailJobStatement, buildMessageBatchMapperFailJob(yourbatis.DialectPostgres, failJob), "MessageBatchMapper.FailJob", yourbatis.StatementUpdate, []any{"retry", failJob.RunAfter, 2, "upstream unavailable", jobUUID}, []string{"status = $1", "jsonb_build_object", "WHERE uuid = $5"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.statement.ID != test.id || test.statement.Kind != test.kind || test.statement.Source == "" {
				t.Fatalf("statement = %+v, want ID %q, kind %q, and source", test.statement, test.id, test.kind)
			}
			if values := test.bound.Values(); !reflect.DeepEqual(values, test.values) {
				t.Fatalf("values = %#v, want %#v", values, test.values)
			}
			for _, fragment := range test.fragments {
				if !strings.Contains(test.bound.SQL, fragment) {
					t.Fatalf("SQL = %q, want fragment %q", test.bound.SQL, fragment)
				}
			}
			if strings.Contains(test.bound.SQL, "::") || strings.Contains(test.bound.SQL, " AS uuid)") {
				t.Fatalf("SQL contains forbidden cast syntax: %q", test.bound.SQL)
			}
		})
	}
}

func TestMessageBatchMapperListPageDirections(t *testing.T) {
	workspaceUUID := "00000000-0000-4000-8000-000000000001"
	anchor := &messageBatchPageAnchor{
		UUID:      "00000000-0000-4000-8000-000000000002",
		CreatedAt: time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC),
	}
	tests := []struct {
		name    string
		before  bool
		wantSQL string
	}{
		{name: "after", wantSQL: "(mb.created_at, mb.uuid) < ($2, $3)"},
		{name: "before", before: true, wantSQL: "(mb.created_at, mb.uuid) > ($2, $3)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bound := buildMessageBatchMapperListPage(yourbatis.DialectPostgres, workspaceUUID, anchor, test.before, 21)
			if values := bound.Values(); !reflect.DeepEqual(values, []any{workspaceUUID, anchor.CreatedAt, anchor.UUID, 21}) {
				t.Fatalf("values = %#v", values)
			}
			if !strings.Contains(bound.SQL, test.wantSQL) || !strings.Contains(bound.SQL, "ORDER BY mb.created_at DESC, mb.uuid DESC") {
				t.Fatalf("SQL = %q, want cursor %q and stable ordering", bound.SQL, test.wantSQL)
			}
		})
	}
}

func TestMessageBatchMapperSensitiveArguments(t *testing.T) {
	result := json.RawMessage(`{"type":"error"}`)
	tests := []struct {
		name      string
		bound     yourbatis.BoundSQL
		sensitive string
	}{
		{"request params", buildMessageBatchMapperInsertRequest(yourbatis.DialectPostgres, insertMessageBatchRequestParams{Params: result}), "params.Params"},
		{"final result", buildMessageBatchMapperFinalizePendingRequests(yourbatis.DialectPostgres, "batch", "errored", result), "result"},
		{"stale result", buildMessageBatchMapperMarkStaleInFlightRequestsErrored(yourbatis.DialectPostgres, "batch", time.Time{}, result), "result"},
		{"request result", buildMessageBatchMapperCompleteRequest(yourbatis.DialectPostgres, completeMessageBatchRequestParams{Result: result}), "params.Result"},
		{"job reason", buildMessageBatchMapperFailJob(yourbatis.DialectPostgres, failMessageBatchJobParams{Reason: "secret upstream detail"}), "params.Reason"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			found := false
			for _, argument := range test.bound.Args {
				if argument.Name == test.sensitive {
					if !argument.Sensitive {
						t.Fatalf("argument %q is not sensitive", argument.Name)
					}
					found = true
					continue
				}
				if argument.Sensitive {
					t.Fatalf("argument %q is unexpectedly sensitive", argument.Name)
				}
			}
			if !found {
				t.Fatalf("sensitive argument %q not found", test.sensitive)
			}
		})
	}
}

func TestMessageBatchMapperResultSemantics(t *testing.T) {
	ctx := context.Background()

	t.Run("single row not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: messageBatchMapperTestColumns()})
		_, err := NewMessageBatchMapper(executor).FindByUUID(ctx, "00000000-0000-4000-8000-000000000001")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindByUUID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("optional row not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "created_at"}})
		_, found, err := NewMessageBatchMapper(executor).FindPageAnchorByExternalID(ctx, "workspace", "batch")
		if err != nil || found {
			t.Fatalf("FindPageAnchorByExternalID() = (%t, %v), want false, nil", found, err)
		}
	})

	t.Run("returning row scan", func(t *testing.T) {
		now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"uuid", "created_at", "updated_at"},
			rows:    [][]driver.Value{{"00000000-0000-4000-8000-000000000001", now, now}},
		})
		row, err := NewMessageBatchMapper(executor).Insert(ctx, insertMessageBatchParams{})
		if err != nil || row.UUID == "" || !row.CreatedAt.Equal(now) {
			t.Fatalf("Insert() = (%+v, %v)", row, err)
		}
	})

	t.Run("scalar scan", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"exists"}, rows: [][]driver.Value{{true}}})
		exists, err := NewMessageBatchMapper(executor).ExistsByExternalID(ctx, "workspace", "batch")
		if err != nil || !exists {
			t.Fatalf("ExistsByExternalID() = (%t, %v)", exists, err)
		}
	})

	t.Run("rows affected", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		rows, err := NewMessageBatchMapper(executor).ClaimRequest(ctx, "request", "worker", time.Time{})
		if err != nil || rows != 1 {
			t.Fatalf("ClaimRequest() = (%d, %v)", rows, err)
		}
	})

	t.Run("execution error", func(t *testing.T) {
		wantErr := errors.New("insert request failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{execErr: wantErr})
		err := NewMessageBatchMapper(executor).InsertRequest(ctx, insertMessageBatchRequestParams{})
		if !errors.Is(err, wantErr) {
			t.Fatalf("InsertRequest() error = %v, want %v", err, wantErr)
		}
	})
}

func messageBatchMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "created_by_api_key_uuid", "api_variant",
		"anthropic_version", "beta_headers", "processing_status", "request_count",
		"processing_count", "succeeded_count", "errored_count", "canceled_count",
		"expired_count", "results_s3_bucket", "results_s3_key", "results_size_bytes",
		"results_sha256", "created_at", "expires_at", "ended_at", "cancel_initiated_at",
		"archived_at", "deleted_at", "last_error", "updated_at",
	}
}

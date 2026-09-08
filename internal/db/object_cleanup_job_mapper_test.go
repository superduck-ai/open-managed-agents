package db

import (
	"context"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestObjectCleanupJobMapperMethodsPropagateExecutionErrors(t *testing.T) {
	ctx := context.Background()
	failureParams := objectCleanupJobFailureParams{}
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{name: "complete cleanup", contract: mapperExecutionErrorContract{statementID: "ObjectCleanupJobMapper.CompleteObjectCleanupJob", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewObjectCleanupJobMapper(executor).CompleteObjectCleanupJob(ctx, "")
		}}},
		{name: "fail cleanup", contract: mapperExecutionErrorContract{statementID: "ObjectCleanupJobMapper.FailObjectCleanupJob", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewObjectCleanupJobMapper(executor).FailObjectCleanupJob(ctx, failureParams)
		}}},
		{name: "enqueue cleanup", contract: mapperExecutionErrorContract{statementID: "ObjectCleanupJobMapper.EnqueueObjectCleanupJob", kind: yourbatis.StatementInsert, call: func(executor yourbatis.Executor) error {
			return NewObjectCleanupJobMapper(executor).EnqueueObjectCleanupJob(ctx, "", nil)
		}}},
		{name: "lease cleanup", contract: mapperExecutionErrorContract{statementID: "ObjectCleanupJobMapper.LeaseObjectCleanupJobs", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewObjectCleanupJobMapper(executor).LeaseObjectCleanupJobs(ctx, "", 0)
			return err
		}}},
		{name: "scheduled enqueue", contract: mapperExecutionErrorContract{statementID: "ObjectCleanupJobMapper.EnqueueScheduledObjectCleanupJob", kind: yourbatis.StatementInsert, call: func(executor yourbatis.Executor) error {
			return NewObjectCleanupJobMapper(executor).EnqueueScheduledObjectCleanupJob(ctx, scheduledObjectCleanupJobParams{})
		}}},
		{name: "expedite", contract: mapperExecutionErrorContract{statementID: "ObjectCleanupJobMapper.ExpediteObjectCleanupJob", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewObjectCleanupJobMapper(executor).ExpediteObjectCleanupJob(ctx, "job_test")
			return err
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperExecutionError(t, test.contract) })
	}
}

func TestObjectCleanupJobMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	failureParams := objectCleanupJobFailureParams{JobUUID: "job-uuid", Status: "retry", RunAfter: now, Attempts: 2, Reason: "temporary"}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{
			name: "complete object cleanup job",
			contract: mapperBuilderContract{
				statement:         objectCleanupJobMapperCompleteObjectCleanupJobStatement,
				bound:             buildObjectCleanupJobMapperCompleteObjectCleanupJob(yourbatis.DialectPostgres, "job-uuid"),
				wantID:            "ObjectCleanupJobMapper.CompleteObjectCleanupJob",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"jobUUID"},
				wantSQLFragments:  []string{"UPDATE jobs", "status = 'completed'", "uuid = $1"},
			},
		},
		{
			name: "fail object cleanup job",
			contract: mapperBuilderContract{
				statement:         objectCleanupJobMapperFailObjectCleanupJobStatement,
				bound:             buildObjectCleanupJobMapperFailObjectCleanupJob(yourbatis.DialectPostgres, failureParams),
				wantID:            "ObjectCleanupJobMapper.FailObjectCleanupJob",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.Status", "params.RunAfter", "params.Attempts", "params.Reason", "params.JobUUID"},
				wantSQLFragments:  []string{"UPDATE jobs", "status = $1", "run_after = $2", "uuid = $5"},
			},
		},
		{name: "enqueue cleanup", contract: mapperBuilderContract{
			statement:         objectCleanupJobMapperEnqueueObjectCleanupJobStatement,
			bound:             buildObjectCleanupJobMapperEnqueueObjectCleanupJob(yourbatis.DialectPostgres, "workspace-uuid", []byte(`{"bucket":"files"}`)),
			wantID:            "ObjectCleanupJobMapper.EnqueueObjectCleanupJob",
			wantKind:          yourbatis.StatementInsert,
			wantArgumentNames: []string{"workspaceUUID", "payload"},
			wantSQLFragments:  []string{"INSERT INTO jobs", "CAST($2 AS jsonb)"},
		}},
		{name: "lease cleanup", contract: mapperBuilderContract{
			statement:         objectCleanupJobMapperLeaseObjectCleanupJobsStatement,
			bound:             buildObjectCleanupJobMapperLeaseObjectCleanupJobs(yourbatis.DialectPostgres, "worker", 10),
			wantID:            "ObjectCleanupJobMapper.LeaseObjectCleanupJobs",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"limit", "workerID"},
			wantSQLFragments:  []string{"FOR UPDATE SKIP LOCKED", "locked_by = $2", "RETURNING"},
		}},
		{name: "scheduled enqueue", contract: mapperBuilderContract{
			statement:         objectCleanupJobMapperEnqueueScheduledObjectCleanupJobStatement,
			bound:             buildObjectCleanupJobMapperEnqueueScheduledObjectCleanupJob(yourbatis.DialectPostgres, scheduledObjectCleanupJobParams{ExternalID: "job_test", WorkspaceUUID: "workspace-uuid", Payload: []byte(`{"bucket":"files"}`), RunAfter: now}),
			wantID:            "ObjectCleanupJobMapper.EnqueueScheduledObjectCleanupJob",
			wantKind:          yourbatis.StatementInsert,
			wantArgumentNames: []string{"params.ExternalID", "params.WorkspaceUUID", "params.Payload", "params.RunAfter"},
			wantSQLFragments:  []string{"INSERT INTO jobs", "CAST($3 AS jsonb)", "$4"},
		}},
		{name: "expedite", contract: mapperBuilderContract{
			statement:         objectCleanupJobMapperExpediteObjectCleanupJobStatement,
			bound:             buildObjectCleanupJobMapperExpediteObjectCleanupJob(yourbatis.DialectPostgres, "job_test"),
			wantID:            "ObjectCleanupJobMapper.ExpediteObjectCleanupJob",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"externalID"},
			wantSQLFragments:  []string{"UPDATE jobs", "status = 'completed'", "external_id = $1", "type = 'object_cleanup'"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}

func TestObjectCleanupJobMapperResultSemantics(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(ObjectCleanupJobMapper) error
	}{
		{name: "complete cleanup success", call: func(mapper ObjectCleanupJobMapper) error {
			return mapper.CompleteObjectCleanupJob(context.Background(), "job-uuid")
		}},
		{name: "fail cleanup success", call: func(mapper ObjectCleanupJobMapper) error {
			return mapper.FailObjectCleanupJob(context.Background(), objectCleanupJobFailureParams{})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
			if err := test.call(NewObjectCleanupJobMapper(executor)); err != nil {
				t.Fatalf("cleanup mutation error = %v", err)
			}
		})
	}
	for _, affected := range []int64{0, 1} {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: affected})
		rows, err := NewObjectCleanupJobMapper(executor).ExpediteObjectCleanupJob(context.Background(), "job_test")
		if err != nil || rows != affected {
			t.Fatalf("ExpediteObjectCleanupJob = (%d, %v), want (%d, nil)", rows, err, affected)
		}
	}
}

package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestFilestoreCleanupMapperBuildsPostgresArguments(t *testing.T) {
	retiredAt := time.Date(2026, time.July, 23, 16, 0, 0, 0, time.UTC)
	batch := filestoreFilesystemBatchMapperParams{
		JobUUID:        "00000000-0000-4000-8000-000000000017",
		JobType:        filestoreFilesystemCleanupJobType,
		LeaseToken:     "filesystem-cleanup-worker",
		WorkspaceUUID:  "00000000-0000-4000-8000-000000000042",
		FilesystemUUID: "00000000-0000-4000-8000-000000000043",
		SessionUUID:    "00000000-0000-4000-8000-000000000045",
		Limit:          100,
		RetiredAt:      retiredAt,
		Status:         "completed",
	}

	tests := []struct {
		name         string
		bound        yourbatis.BoundSQL
		wantArgCount int
		wantClauses  []string
	}{
		{
			name: "leased filesystem job",
			bound: buildFilestoreCleanupMapperGetLeasedFilesystemJob(yourbatis.DialectPostgres, filestoreCleanupJobLeaseIdentity{
				JobUUID:    batch.JobUUID,
				JobType:    batch.JobType,
				LeaseToken: batch.LeaseToken,
			}),
			wantArgCount: 3,
			wantClauses:  []string{"j.status = 'running'", "j.locked_by = $3", "FOR UPDATE OF j"},
		},
		{
			name:         "filesystem entries",
			bound:        buildFilestoreCleanupMapperListFilesystemFiles(yourbatis.DialectPostgres, batch),
			wantArgCount: 3,
			wantClauses:  []string{"LEFT JOIN files", "kind = 'file'", "ORDER BY uuid", "LIMIT $3"},
		},
		{
			name:         "retire namespace",
			bound:        buildFilestoreCleanupMapperRetireNamespace(yourbatis.DialectPostgres, batch),
			wantArgCount: 4,
			wantClauses:  []string{"resource_type IN ('directory', 'skill_archive')", "deleted_at = $1"},
		},
		{
			name:         "complete filesystem batch",
			bound:        buildFilestoreCleanupMapperCompleteFilesystemBatch(yourbatis.DialectPostgres, batch),
			wantArgCount: 6,
			wantClauses:  []string{"payload = payload - 'lease_attempts'", "locked_by = $6"},
		},
		{
			name: "lease object jobs",
			bound: buildFilestoreCleanupMapperLeaseObjectJobs(yourbatis.DialectPostgres, filestoreCleanupJobLeaseParams{
				JobType:          filestoreCleanupJobType,
				WorkerID:         "cleanup-worker",
				Limit:            10,
				MaxLeaseAttempts: 5,
			}),
			wantClauses: []string{"FOR UPDATE OF j SKIP LOCKED", "leased_jobs AS", "lease_attempts"},
		},
		{
			name: "cancel attached object",
			bound: buildFilestoreCleanupMapperCancelAttachedObjectJob(yourbatis.DialectPostgres, filestoreCleanupJobMutationParams{
				JobExternalID: "job_test",
				JobType:       filestoreCleanupJobType,
				WorkspaceUUID: batch.WorkspaceUUID,
				Bucket:        "objects",
				Key:           "key",
				VersionID:     "version",
			}),
			wantArgCount: 6,
			wantClauses:  []string{"status IN ('pending', 'retry')", "payload->>'version_id'"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.wantArgCount > 0 && len(test.bound.Args) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(test.bound.Args), test.wantArgCount)
			}
			if strings.Contains(test.bound.SQL, ":") {
				t.Fatalf("bound query retains named parameter syntax: %q", test.bound.SQL)
			}
			for _, clause := range test.wantClauses {
				if !strings.Contains(test.bound.SQL, clause) {
					t.Fatalf("bound query does not contain %q: %q", clause, test.bound.SQL)
				}
			}
		})
	}
}

func TestFilestoreCleanupMapperJobAndSubtreeBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	batchParams := filestoreFilesystemBatchMapperParams{
		WorkspaceUUID: "workspace-uuid",
		SessionUUID:   "session-uuid",
		RetiredAt:     now,
	}
	insertParams := filestoreCleanupJobInsertParams{
		WorkspaceUUID:   "workspace-uuid",
		FilesystemUUID:  "filesystem-uuid",
		JobType:         "cleanup",
		EntryExternalID: "entry-external-id",
		Bucket:          "objects",
		Key:             "object-key",
		ETag:            "etag",
		VersionID:       "version",
		Reason:          "expired",
		RunAfter:        now,
	}
	leaseParams := filestoreCleanupJobLeaseParams{JobType: "cleanup", WorkerID: "worker", Limit: 10, MaxLeaseAttempts: 5}
	mutationParams := filestoreCleanupJobMutationParams{
		JobUUID:       "job-uuid",
		JobExternalID: "job-external-id",
		JobType:       "cleanup",
		WorkspaceUUID: "workspace-uuid",
		LeaseToken:    "worker",
		ETag:          "etag",
		VersionID:     "version",
		Reason:        "temporary",
		RunAfter:      now,
		MaxAttempts:   3,
	}
	subtreeParams := filestoreSubtreeMapperParams{WorkspaceUUID: "workspace-uuid", SessionUUID: "session-uuid", RootPath: "/outputs/root"}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{
			name: "get filesystem for cleanup",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperGetFilesystemForCleanupStatement,
				bound:             buildFilestoreCleanupMapperGetFilesystemForCleanup(yourbatis.DialectPostgres, "workspace-uuid", "filesystem-uuid"),
				wantID:            "FilestoreCleanupMapper.GetFilesystemForCleanup",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "filesystemUUID"},
				wantSQLFragments:  []string{"FROM filestore_filesystems", "workspace_uuid = $1", "uuid = $2"},
			},
		},
		{
			name: "filesystem files remain",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperFilesystemFilesRemainStatement,
				bound:             buildFilestoreCleanupMapperFilesystemFilesRemain(yourbatis.DialectPostgres, "workspace-uuid", "session-uuid"),
				wantID:            "FilestoreCleanupMapper.FilesystemFilesRemain",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "sessionUUID"},
				wantSQLFragments:  []string{"SELECT EXISTS", "resource_type = 'file'", "AS files_remain"},
			},
		},
		{
			name: "retire skill files",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperRetireSkillFilesStatement,
				bound:             buildFilestoreCleanupMapperRetireSkillFiles(yourbatis.DialectPostgres, batchParams),
				wantID:            "FilestoreCleanupMapper.RetireSkillFiles",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.RetiredAt", "params.WorkspaceUUID", "params.WorkspaceUUID", "params.SessionUUID"},
				wantSQLFragments:  []string{"UPDATE files file", "resource_type = 'skill_archive'", "resource.session_uuid = $4"},
			},
		},
		{
			name: "insert filesystem job",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperInsertFilesystemJobStatement,
				bound:             buildFilestoreCleanupMapperInsertFilesystemJob(yourbatis.DialectPostgres, insertParams),
				wantID:            "FilestoreCleanupMapper.InsertFilesystemJob",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.JobType", "params.WorkspaceUUID", "params.FilesystemUUID", "params.RunAfter"},
				wantSQLFragments:  []string{"WITH inserted_job AS", "INSERT INTO jobs", "RETURNING uuid, external_id", "JOIN filestore_filesystems fs"},
			},
		},
		{
			name: "insert object job",
			contract: mapperBuilderContract{
				statement: filestoreCleanupMapperInsertObjectJobStatement,
				bound:     buildFilestoreCleanupMapperInsertObjectJob(yourbatis.DialectPostgres, insertParams),
				wantID:    "FilestoreCleanupMapper.InsertObjectJob",
				wantKind:  yourbatis.StatementSelect,
				wantArgumentNames: []string{
					"params.WorkspaceUUID", "params.JobType", "params.WorkspaceUUID", "params.FilesystemUUID", "params.EntryExternalID",
					"params.Bucket", "params.Key", "params.ETag", "params.VersionID", "params.Reason", "params.RunAfter",
				},
				wantSQLFragments: []string{"WITH inserted_job AS", "jsonb_build_object", "'entry_external_id'", "RETURNING uuid, external_id"},
			},
		},
		{
			name: "lease filesystem jobs",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperLeaseFilesystemJobsStatement,
				bound:             buildFilestoreCleanupMapperLeaseFilesystemJobs(yourbatis.DialectPostgres, leaseParams),
				wantID:            "FilestoreCleanupMapper.LeaseFilesystemJobs",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.JobType", "params.MaxLeaseAttempts", "params.Limit", "params.JobType", "params.MaxLeaseAttempts", "params.Limit", "params.WorkerID"},
				wantSQLFragments:  []string{"FOR UPDATE OF j SKIP LOCKED", "leased_jobs AS", "locked_by = $7"},
			},
		},
		{
			name: "attach object version",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperAttachObjectVersionStatement,
				bound:             buildFilestoreCleanupMapperAttachObjectVersion(yourbatis.DialectPostgres, mutationParams),
				wantID:            "FilestoreCleanupMapper.AttachObjectVersion",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.ETag", "params.VersionID", "params.JobExternalID", "params.JobType", "params.WorkspaceUUID"},
				wantSQLFragments:  []string{"jsonb_build_object", "external_id = $3", "status IN ('pending', 'retry')"},
			},
		},
		{
			name: "complete pending object job",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperCompletePendingObjectJobStatement,
				bound:             buildFilestoreCleanupMapperCompletePendingObjectJob(yourbatis.DialectPostgres, mutationParams),
				wantID:            "FilestoreCleanupMapper.CompletePendingObjectJob",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.JobUUID", "params.JobType"},
				wantSQLFragments:  []string{"status = 'completed'", "uuid = $1", "status IN ('pending', 'retry')"},
			},
		},
		{
			name: "complete leased object job",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperCompleteLeasedObjectJobStatement,
				bound:             buildFilestoreCleanupMapperCompleteLeasedObjectJob(yourbatis.DialectPostgres, mutationParams),
				wantID:            "FilestoreCleanupMapper.CompleteLeasedObjectJob",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.JobUUID", "params.JobType", "params.LeaseToken"},
				wantSQLFragments:  []string{"status = 'completed'", "status = 'running'", "locked_by = $3"},
			},
		},
		{
			name: "fail leased job",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperFailLeasedJobStatement,
				bound:             buildFilestoreCleanupMapperFailLeasedJob(yourbatis.DialectPostgres, mutationParams),
				wantID:            "FilestoreCleanupMapper.FailLeasedJob",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.MaxAttempts", "params.RunAfter", "params.Reason", "params.JobUUID", "params.JobType", "params.LeaseToken"},
				wantSQLFragments:  []string{"attempts + 1 >= $1", "run_after = $2", "'last_error'", "locked_by = $6"},
			},
		},
		{
			name: "cancel pending object job",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperCancelPendingObjectJobStatement,
				bound:             buildFilestoreCleanupMapperCancelPendingObjectJob(yourbatis.DialectPostgres, mutationParams),
				wantID:            "FilestoreCleanupMapper.CancelPendingObjectJob",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.JobExternalID", "params.JobType", "params.WorkspaceUUID"},
				wantSQLFragments:  []string{"status = 'canceled'", "external_id = $1", "workspace_uuid' = CAST($3 AS text)"},
			},
		},
		{
			name: "get object job status",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperGetObjectJobStatusStatement,
				bound:             buildFilestoreCleanupMapperGetObjectJobStatus(yourbatis.DialectPostgres, mutationParams),
				wantID:            "FilestoreCleanupMapper.GetObjectJobStatus",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.JobExternalID", "params.JobType", "params.WorkspaceUUID"},
				wantSQLFragments:  []string{"SELECT status FROM jobs", "external_id = $1", "CAST($3 AS text)"},
			},
		},
		{
			name: "list subtree files",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperListSubtreeFilesStatement,
				bound:             buildFilestoreCleanupMapperListSubtreeFiles(yourbatis.DialectPostgres, subtreeParams),
				wantID:            "FilestoreCleanupMapper.ListSubtreeFiles",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.RootPath", "params.RootPath"},
				wantSQLFragments:  []string{"kind = 'file'", "left(path, char_length($3) + 1) = $4 || '/'", "ORDER BY id"},
			},
		},
		{
			name: "list expired subtree files",
			contract: mapperBuilderContract{
				statement:         filestoreCleanupMapperListExpiredSubtreeFilesStatement,
				bound:             buildFilestoreCleanupMapperListExpiredSubtreeFiles(yourbatis.DialectPostgres, subtreeParams),
				wantID:            "FilestoreCleanupMapper.ListExpiredSubtreeFiles",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.RootPath", "params.RootPath", "params.RootPath"},
				wantSQLFragments:  []string{"expires_at <= now()", "path = $3", "left(path, char_length($4) + 1) = $5 || '/'"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
		})
	}
}

func TestFilestoreCleanupMapperScopeAndLeaseBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	batchParams := filestoreFilesystemBatchMapperParams{
		JobUUID:       "job-uuid",
		JobType:       "cleanup",
		LeaseToken:    "worker",
		WorkspaceUUID: "workspace-uuid",
		SessionUUID:   "session-uuid",
		Limit:         10,
		RetiredAt:     now,
		Status:        "completed",
	}
	leaseIdentity := filestoreCleanupJobLeaseIdentity{JobUUID: "job-uuid", JobType: "cleanup", LeaseToken: "worker"}
	leaseParams := filestoreCleanupJobLeaseParams{JobType: "cleanup", WorkerID: "worker", Limit: 10, MaxLeaseAttempts: 5}
	mutationParams := filestoreCleanupJobMutationParams{
		JobExternalID: "job-external-id",
		JobType:       "cleanup",
		WorkspaceUUID: "workspace-uuid",
		Bucket:        "objects",
		Key:           "object-key",
		VersionID:     "version",
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{name: "get leased filesystem job", contract: mapperBuilderContract{
			statement:         filestoreCleanupMapperGetLeasedFilesystemJobStatement,
			bound:             buildFilestoreCleanupMapperGetLeasedFilesystemJob(yourbatis.DialectPostgres, leaseIdentity),
			wantID:            "FilestoreCleanupMapper.GetLeasedFilesystemJob",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.JobUUID", "params.JobType", "params.LeaseToken"},
			wantSQLFragments:  []string{"j.status = 'running'", "j.locked_by = $3", "FOR UPDATE OF j"},
		}},
		{name: "list filesystem files", contract: mapperBuilderContract{
			statement:         filestoreCleanupMapperListFilesystemFilesStatement,
			bound:             buildFilestoreCleanupMapperListFilesystemFiles(yourbatis.DialectPostgres, batchParams),
			wantID:            "FilestoreCleanupMapper.ListFilesystemFiles",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.Limit"},
			wantSQLFragments:  []string{"kind = 'file'", "ORDER BY uuid", "LIMIT $3"},
		}},
		{name: "retire namespace", contract: mapperBuilderContract{
			statement:         filestoreCleanupMapperRetireNamespaceStatement,
			bound:             buildFilestoreCleanupMapperRetireNamespace(yourbatis.DialectPostgres, batchParams),
			wantID:            "FilestoreCleanupMapper.RetireNamespace",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.RetiredAt", "params.RetiredAt", "params.WorkspaceUUID", "params.SessionUUID"},
			wantSQLFragments:  []string{"resource_type IN ('directory', 'skill_archive')", "deleted_at = $1", "updated_at = $2"},
		}},
		{name: "complete filesystem batch", contract: mapperBuilderContract{
			statement:         filestoreCleanupMapperCompleteFilesystemBatchStatement,
			bound:             buildFilestoreCleanupMapperCompleteFilesystemBatch(yourbatis.DialectPostgres, batchParams),
			wantID:            "FilestoreCleanupMapper.CompleteFilesystemBatch",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.Status", "params.RetiredAt", "params.RetiredAt", "params.JobUUID", "params.JobType", "params.LeaseToken"},
			wantSQLFragments:  []string{"payload = payload - 'lease_attempts'", "uuid = $4", "locked_by = $6"},
		}},
		{name: "lease object jobs", contract: mapperBuilderContract{
			statement:         filestoreCleanupMapperLeaseObjectJobsStatement,
			bound:             buildFilestoreCleanupMapperLeaseObjectJobs(yourbatis.DialectPostgres, leaseParams),
			wantID:            "FilestoreCleanupMapper.LeaseObjectJobs",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.JobType", "params.MaxLeaseAttempts", "params.Limit", "params.JobType", "params.MaxLeaseAttempts", "params.Limit", "params.WorkerID"},
			wantSQLFragments:  []string{"FOR UPDATE OF j SKIP LOCKED", "leased_jobs AS", "locked_by = $7"},
		}},
		{name: "cancel attached object job", contract: mapperBuilderContract{
			statement:         filestoreCleanupMapperCancelAttachedObjectJobStatement,
			bound:             buildFilestoreCleanupMapperCancelAttachedObjectJob(yourbatis.DialectPostgres, mutationParams),
			wantID:            "FilestoreCleanupMapper.CancelAttachedObjectJob",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.JobExternalID", "params.JobType", "params.WorkspaceUUID", "params.Bucket", "params.Key", "params.VersionID"},
			wantSQLFragments:  []string{"status = 'canceled'", "payload->>'bucket' = $4", "payload->>'key' = $5"},
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
		})
	}
}

func TestFilestoreCleanupMapperJobAndSubtreeMethodsPropagateExecutionErrors(t *testing.T) {
	ctx := context.Background()
	batchParams := filestoreFilesystemBatchMapperParams{}
	insertParams := filestoreCleanupJobInsertParams{}
	leaseParams := filestoreCleanupJobLeaseParams{}
	mutationParams := filestoreCleanupJobMutationParams{}
	subtreeParams := filestoreSubtreeMapperParams{}
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{name: "get filesystem", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.GetFilesystemForCleanup", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).GetFilesystemForCleanup(ctx, "", "")
			return err
		}}},
		{name: "files remain", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.FilesystemFilesRemain", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).FilesystemFilesRemain(ctx, "", "")
			return err
		}}},
		{name: "retire skill files", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.RetireSkillFiles", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewFilestoreCleanupMapper(executor).RetireSkillFiles(ctx, batchParams)
		}}},
		{name: "insert filesystem job", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.InsertFilesystemJob", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).InsertFilesystemJob(ctx, insertParams)
			return err
		}}},
		{name: "insert object job", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.InsertObjectJob", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).InsertObjectJob(ctx, insertParams)
			return err
		}}},
		{name: "lease filesystem jobs", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.LeaseFilesystemJobs", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).LeaseFilesystemJobs(ctx, leaseParams)
			return err
		}}},
		{name: "attach object version", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.AttachObjectVersion", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).AttachObjectVersion(ctx, mutationParams)
			return err
		}}},
		{name: "complete pending job", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.CompletePendingObjectJob", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).CompletePendingObjectJob(ctx, mutationParams)
			return err
		}}},
		{name: "complete leased job", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.CompleteLeasedObjectJob", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).CompleteLeasedObjectJob(ctx, mutationParams)
			return err
		}}},
		{name: "fail leased job", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.FailLeasedJob", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).FailLeasedJob(ctx, mutationParams)
			return err
		}}},
		{name: "cancel pending job", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.CancelPendingObjectJob", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).CancelPendingObjectJob(ctx, mutationParams)
			return err
		}}},
		{name: "get object status", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.GetObjectJobStatus", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).GetObjectJobStatus(ctx, mutationParams)
			return err
		}}},
		{name: "list subtree files", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.ListSubtreeFiles", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).ListSubtreeFiles(ctx, subtreeParams)
			return err
		}}},
		{name: "list expired subtree files", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.ListExpiredSubtreeFiles", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).ListExpiredSubtreeFiles(ctx, subtreeParams)
			return err
		}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperExecutionError(t, test.contract)
		})
	}
}

func TestFilestoreCleanupMapperScopeAndLeaseMethodsPropagateExecutionErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{name: "get leased filesystem job", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.GetLeasedFilesystemJob", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).GetLeasedFilesystemJob(ctx, filestoreCleanupJobLeaseIdentity{})
			return err
		}}},
		{name: "list filesystem files", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.ListFilesystemFiles", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).ListFilesystemFiles(ctx, filestoreFilesystemBatchMapperParams{})
			return err
		}}},
		{name: "retire namespace", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.RetireNamespace", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewFilestoreCleanupMapper(executor).RetireNamespace(ctx, filestoreFilesystemBatchMapperParams{})
		}}},
		{name: "complete filesystem batch", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.CompleteFilesystemBatch", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).CompleteFilesystemBatch(ctx, filestoreFilesystemBatchMapperParams{})
			return err
		}}},
		{name: "lease object jobs", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.LeaseObjectJobs", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).LeaseObjectJobs(ctx, filestoreCleanupJobLeaseParams{})
			return err
		}}},
		{name: "cancel attached object job", contract: mapperExecutionErrorContract{statementID: "FilestoreCleanupMapper.CancelAttachedObjectJob", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewFilestoreCleanupMapper(executor).CancelAttachedObjectJob(ctx, filestoreCleanupJobMutationParams{})
			return err
		}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperExecutionError(t, test.contract)
		})
	}
}

func TestFilestoreCleanupMapperResultSemantics(t *testing.T) {
	t.Run("required job scan error", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"uuid"},
			rows:    [][]driver.Value{{"invalid-uuid"}},
		})
		_, err := NewFilestoreCleanupMapper(executor).InsertFilesystemJob(context.Background(), filestoreCleanupJobInsertParams{})
		if err == nil {
			t.Fatal("InsertFilesystemJob error = nil, want scan error")
		}
	})

	t.Run("required job missing", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}})
		_, err := NewFilestoreCleanupMapper(executor).InsertFilesystemJob(context.Background(), filestoreCleanupJobInsertParams{})
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("InsertFilesystemJob error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("empty leased jobs", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}})
		rows, err := NewFilestoreCleanupMapper(executor).LeaseFilesystemJobs(context.Background(), filestoreCleanupJobLeaseParams{})
		if err != nil || len(rows) != 0 {
			t.Fatalf("LeaseFilesystemJobs = (%#v, %v), want empty list and nil error", rows, err)
		}
	})

	t.Run("rows affected", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 3})
		rowsAffected, err := NewFilestoreCleanupMapper(executor).CompletePendingObjectJob(context.Background(), filestoreCleanupJobMutationParams{})
		if err != nil || rowsAffected != 3 {
			t.Fatalf("CompletePendingObjectJob = (%d, %v), want (3, nil)", rowsAffected, err)
		}
	})
}

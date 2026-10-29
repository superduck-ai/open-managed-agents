package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestCodeSessionClaimMapperBuilderContracts(t *testing.T) {
	expiresAt := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"lock next claimable", mapperBuilderContract{
			statement: codeSessionClaimMapperLockNextClaimableStatement,
			bound:     buildCodeSessionClaimMapperLockNextClaimable(yourbatis.DialectPostgres, 10_000_000),
			wantID:    "CodeSessionClaimMapper.LockNextClaimable", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"leaseGraceMicros"},
			wantSQLFragments: []string{
				"worker_transport}' = 'claim'", "worker_lease_expires_at + $1 * INTERVAL '1 microsecond'",
				"s.status IN ('running', 'rescheduling')", "s.organization_uuid = cs.organization_uuid", "AND s.uuid IS NOT NULL",
				"FOR UPDATE OF cs SKIP LOCKED",
				"CASE WHEN cs.worker_claim_requested_at IS NOT NULL THEN cs.worker_claim_trace_parent END AS worker_claim_trace_parent",
			},
		}},
		{"claim", mapperBuilderContract{
			statement: codeSessionClaimMapperClaimStatement,
			bound: buildCodeSessionClaimMapperClaim(yourbatis.DialectPostgres, claimCodeSessionParams{
				UUID: "code-session-uuid", WorkerID: "worker", ClaimExpiresAt: expiresAt, OAuthAccessTokenHash: "hash",
			}),
			wantID: "CodeSessionClaimMapper.Claim", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames:          []string{"params.OAuthAccessTokenHash", "params.WorkerID", "params.ClaimExpiresAt", "params.UUID"},
			wantSensitiveArgumentNames: []string{"params.OAuthAccessTokenHash"},
			wantSQLFragments:           []string{"current_worker_epoch = current_worker_epoch + 1", "worker_lease_expires_at = NULL", "RETURNING current_worker_epoch"},
		}},
		{"release after failure", mapperBuilderContract{
			statement: codeSessionClaimMapperReleaseStatement,
			bound: buildCodeSessionClaimMapperRelease(yourbatis.DialectPostgres, releaseCodeSessionClaimParams{
				ExternalID: "cse_test", WorkerID: "worker", Epoch: 2, Kind: CodeSessionClaimReleaseRetry,
			}),
			wantID: "CodeSessionClaimMapper.Release", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.ExternalID", "params.Epoch", "params.WorkerID"},
			wantSQLFragments:  []string{"worker_claim_requested_at = COALESCE(worker_claim_requested_at, NOW())", "LEAST(5 * POWER(2, GREATEST(worker_claim_attempts - 1, 0)), 300)", "worker_claimed_by = $3"},
		}},
		{"release on shutdown", mapperBuilderContract{
			statement: codeSessionClaimMapperReleaseStatement,
			bound: buildCodeSessionClaimMapperRelease(yourbatis.DialectPostgres, releaseCodeSessionClaimParams{
				ExternalID: "cse_test", WorkerID: "worker", Epoch: 2, Kind: CodeSessionClaimReleaseHandoff,
			}),
			wantID: "CodeSessionClaimMapper.Release", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.ExternalID", "params.Epoch", "params.WorkerID"},
			wantSQLFragments:  []string{"worker_claim_requested_at = COALESCE(worker_claim_requested_at, NOW())", "worker_claim_expires_at = NOW()", "worker_claim_attempts = GREATEST(worker_claim_attempts - 1, 0)"},
		}},
		{"release when idle", mapperBuilderContract{
			statement: codeSessionClaimMapperReleaseStatement,
			bound: buildCodeSessionClaimMapperRelease(yourbatis.DialectPostgres, releaseCodeSessionClaimParams{
				ExternalID: "cse_test", WorkerID: "worker", Epoch: 2, Kind: CodeSessionClaimReleaseIdle,
			}),
			wantID: "CodeSessionClaimMapper.Release", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.ExternalID", "params.Epoch", "params.WorkerID"},
			wantSQLFragments:  []string{"worker_lease_expires_at = NULL", "worker_claim_expires_at = NOW()"},
		}},
		{"heartbeat lease lock reads transport", mapperBuilderContract{
			statement: codeSessionMapperLockWorkerLeaseByExternalIDStatement,
			bound:     buildCodeSessionMapperLockWorkerLeaseByExternalID(yourbatis.DialectPostgres, "cse_test"),
			wantID:    "CodeSessionMapper.LockWorkerLeaseByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"codeSessionExternalID"},
			wantSQLFragments:  []string{"metadata #>> '{config,worker_transport}'", "AS worker_transport", "FOR UPDATE"},
		}},
		{"worker claim state", mapperBuilderContract{
			statement: codeSessionClaimMapperFindWorkerClaimStateStatement,
			bound:     buildCodeSessionClaimMapperFindWorkerClaimState(yourbatis.DialectPostgres, "org-uuid", "workspace-uuid", "cse_test"),
			wantID:    "CodeSessionClaimMapper.FindWorkerClaimState", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments: []string{
				"(worker_lease_expires_at IS NULL AND worker_claim_expires_at IS NOT NULL) AS claim_released",
				"organization_uuid = $1", "workspace_uuid = $2", "external_id = $3", "deleted_at IS NULL",
			},
			wantSQLAbsent: []string{"FOR UPDATE", "CAST("},
		}},
		{"reset exhausted", mapperBuilderContract{
			statement: codeSessionClaimMapperResetExhaustedStatement,
			bound:     buildCodeSessionClaimMapperResetExhausted(yourbatis.DialectPostgres, "code-session-uuid", 4),
			wantID:    "CodeSessionClaimMapper.ResetExhausted", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"uuid", "epoch"},
			wantSQLFragments:  []string{"worker_claim_attempts = 0", "current_worker_epoch = $2"},
		}},
		{"hold exhausted", mapperBuilderContract{
			statement: codeSessionClaimMapperHoldExhaustedStatement,
			bound:     buildCodeSessionClaimMapperHoldExhausted(yourbatis.DialectPostgres, "code-session-uuid", expiresAt),
			wantID:    "CodeSessionClaimMapper.HoldExhausted", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"holdUntil", "uuid"},
			wantSQLFragments:  []string{"current_worker_epoch = current_worker_epoch + CASE WHEN worker_claimed_by IS NULL THEN 0 ELSE 1 END", "worker_claimed_by = NULL", "RETURNING current_worker_epoch"},
		}},
		{"fail permanently", mapperBuilderContract{
			statement: codeSessionClaimMapperFailPermanentlyStatement,
			bound: buildCodeSessionClaimMapperFailPermanently(yourbatis.DialectPostgres, failCodeSessionClaimParams{
				ExternalID: "cse_test", WorkerID: "worker", Epoch: 2, MaxAttempts: 5, HoldUntil: expiresAt,
			}),
			wantID: "CodeSessionClaimMapper.FailPermanently", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.HoldUntil", "params.MaxAttempts", "params.ExternalID", "params.Epoch", "params.WorkerID"},
			wantSQLFragments:  []string{"current_worker_epoch = current_worker_epoch + 1", "worker_claimed_by = NULL", "GREATEST(worker_claim_attempts, $2)", "worker_claimed_by = $5", "RETURNING uuid, current_worker_epoch"},
		}},
		{"request", mapperBuilderContract{
			statement: codeSessionClaimMapperRequestStatement,
			bound:     buildCodeSessionClaimMapperRequest(yourbatis.DialectPostgres, "org-uuid", "workspace-uuid", "cse_test", new("00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")),
			wantID:    "CodeSessionClaimMapper.Request", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"traceParent", "organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments: []string{
				"worker_claim_trace_parent = CASE WHEN worker_claim_requested_at IS NULL THEN $1 ELSE worker_claim_trace_parent END",
				"COALESCE(worker_claim_requested_at, NOW())", "status = 'active'",
			},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}

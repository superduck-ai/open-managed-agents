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
				"s.status IN ('running', 'rescheduling')", "s.organization_uuid = cs.organization_uuid",
				"FOR UPDATE OF cs SKIP LOCKED",
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
				ExternalID: "cse_test", WorkerID: "worker", Epoch: 2, Failed: true,
			}),
			wantID: "CodeSessionClaimMapper.Release", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.ExternalID", "params.Epoch", "params.WorkerID"},
			wantSQLFragments:  []string{"LEAST(5 * POWER(2, GREATEST(worker_claim_attempts - 1, 0)), 300)", "worker_claimed_by = $3"},
		}},
		{"release on shutdown", mapperBuilderContract{
			statement: codeSessionClaimMapperReleaseStatement,
			bound: buildCodeSessionClaimMapperRelease(yourbatis.DialectPostgres, releaseCodeSessionClaimParams{
				ExternalID: "cse_test", WorkerID: "worker", Epoch: 2,
			}),
			wantID: "CodeSessionClaimMapper.Release", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.ExternalID", "params.Epoch", "params.WorkerID"},
			wantSQLFragments:  []string{"worker_claim_expires_at = NOW()", "worker_claim_attempts = GREATEST(worker_claim_attempts - 1, 0)"},
		}},
		{"request", mapperBuilderContract{
			statement: codeSessionClaimMapperRequestStatement,
			bound:     buildCodeSessionClaimMapperRequest(yourbatis.DialectPostgres, "org-uuid", "workspace-uuid", "cse_test"),
			wantID:    "CodeSessionClaimMapper.Request", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"COALESCE(worker_claim_requested_at, NOW())", "status = 'active'"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}

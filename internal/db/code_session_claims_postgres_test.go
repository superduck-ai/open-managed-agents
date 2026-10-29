//go:build integration

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

const (
	claimTestOrganization = "53000000-0000-0000-0000-000000000001"
	claimTestWorkspace    = "53000000-0000-0000-0000-000000000002"
)

func newCodeSessionClaimTestStore(t *testing.T) (context.Context, *sql.DB, *DB) {
	t.Helper()
	url := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, url)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, database, &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
}

func seedClaimCodeSession(t *testing.T, ctx context.Context, database *sql.DB, index int, transport, sessionStatus string) string {
	t.Helper()
	sessionUUID := fmt.Sprintf("53000000-0000-0000-0001-%012d", index)
	codeSessionID := fmt.Sprintf("cse_claim_%d", index)
	if _, err := database.ExecContext(ctx, `
		insert into sessions (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			environment_uuid, environment_external_id, agent_uuid, agent_external_id,
			agent_version, agent_snapshot, status
		) values ($1, $2, $3, $4, '53000000-0000-0000-0000-000000000004',
			'53000000-0000-0000-0000-000000000003', 'env_claim', '53000000-0000-0000-0000-000000000005',
			'agent_claim', 1, '{}', $5)`,
		sessionUUID, fmt.Sprintf("sesn_claim_%d", index), claimTestOrganization, claimTestWorkspace, sessionStatus,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		insert into code_sessions (
			external_id, organization_uuid, workspace_uuid, session_uuid, session_external_id,
			environment_uuid, environment_external_id, metadata, current_worker_epoch
		) values ($1, $2, $3, $4, $5, '53000000-0000-0000-0000-000000000003', 'env_claim',
			jsonb_build_object('config', jsonb_build_object('worker_transport', $6::text)), 1)`,
		codeSessionID, claimTestOrganization, claimTestWorkspace, sessionUUID, fmt.Sprintf("sesn_claim_%d", index), transport,
	); err != nil {
		t.Fatalf("seed code session: %v", err)
	}
	return codeSessionID
}

const claimTestTraceParent = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"

func claimTestInput(workerID string) ClaimCodeSessionInput {
	return ClaimCodeSessionInput{
		WorkerID: workerID, OAuthAccessTokenHash: "hash-" + workerID,
		ClaimTTL: 30 * time.Second, LeaseGrace: 10 * time.Second, MaxAttempts: 3, ExhaustedHold: time.Minute,
	}
}

func execClaimTest(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestCodeSessionClaimsPostgresRejectsIneligibleSessions(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	seedClaimCodeSession(t, ctx, database, 1, "nats", "running")
	idle := seedClaimCodeSession(t, ctx, database, 2, "claim", "idle")
	live := seedClaimCodeSession(t, ctx, database, 3, "claim", "running")
	execClaimTest(t, ctx, database, `update code_sessions set worker_lease_expires_at = now() - interval '5 seconds' where external_id = $1`, live)
	deleted := seedClaimCodeSession(t, ctx, database, 4, "claim", "running")
	if err := store.RequestCodeSessionWorker(ctx, claimTestOrganization, claimTestWorkspace, deleted, ""); err != nil {
		t.Fatal(err)
	}
	execClaimTest(t, ctx, database, `update sessions set deleted_at = now() where external_id = 'sesn_claim_4'`)

	if _, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a")); err != nil || found {
		t.Fatalf("ineligible sessions claimed: found=%t err=%v", found, err)
	}
	if err := store.RequestCodeSessionWorker(ctx, claimTestWorkspace, claimTestWorkspace, idle, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign organization request: %v", err)
	}
	if err := store.ReleaseCodeSessionClaim(ctx, idle, "worker-a", 1, CodeSessionClaimReleaseRetry); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("release without claim: %v", err)
	}
}

func TestCodeSessionClaimsPostgresConcurrentClaimsAreExclusive(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeSessionID := seedClaimCodeSession(t, ctx, database, 1, "claim", "idle")
	if err := store.RequestCodeSessionWorker(ctx, claimTestOrganization, claimTestWorkspace, codeSessionID, ""); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, found, err := store.ClaimNextCodeSession(ctx, claimTestInput(fmt.Sprintf("worker-%d", i)))
			if err != nil {
				t.Error(err)
			}
			results <- found
		}()
	}
	wg.Wait()
	close(results)
	claimed := 0
	for found := range results {
		if found {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed %d times, want 1", claimed)
	}
}

func TestCodeSessionClaimsPostgresLifecycle(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeSessionID := seedClaimCodeSession(t, ctx, database, 1, "claim", "idle")
	if err := store.RequestCodeSessionWorker(ctx, claimTestOrganization, claimTestWorkspace, codeSessionID, claimTestTraceParent); err != nil {
		t.Fatal(err)
	}
	if err := store.RequestCodeSessionWorker(ctx, claimTestOrganization, claimTestWorkspace, codeSessionID, "00-ffffffffffffffffffffffffffffffff-ffffffffffffffff-01"); err != nil {
		t.Fatal(err)
	}
	claim, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a"))
	if err != nil || !found || claim.Exhausted || claim.CodeSessionExternalID != codeSessionID || claim.WorkerEpoch != 2 {
		t.Fatalf("first claim = %+v found=%t err=%v", claim, found, err)
	}
	if claim.TraceParent != claimTestTraceParent || claim.RequestedAt == nil {
		t.Fatalf("first claim trace=%q requested_at=%v, want the first request's trace", claim.TraceParent, claim.RequestedAt)
	}
	if _, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-b")); err != nil || found {
		t.Fatalf("claim within claim TTL: found=%t err=%v", found, err)
	}
	if err := store.ReleaseCodeSessionClaim(ctx, codeSessionID, "worker-b", claim.WorkerEpoch, CodeSessionClaimReleaseRetry); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("release by another worker: %v", err)
	}
	if err := store.ReleaseCodeSessionClaim(ctx, codeSessionID, "worker-a", claim.WorkerEpoch, CodeSessionClaimReleaseHandoff); err != nil {
		t.Fatal(err)
	}
	second, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-b"))
	if err != nil || !found || second.WorkerEpoch != 3 {
		t.Fatalf("claim after shutdown release = %+v found=%t err=%v", second, found, err)
	}
	if err := store.ReleaseCodeSessionClaim(ctx, codeSessionID, "worker-b", second.WorkerEpoch, CodeSessionClaimReleaseRetry); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-c")); err != nil || found {
		t.Fatalf("claim during failure backoff: found=%t err=%v", found, err)
	}

	execClaimTest(t, ctx, database, `update code_sessions set worker_claim_expires_at = now() - interval '1 second' where external_id = $1`, codeSessionID)
	third, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-c"))
	if err != nil || !found || third.WorkerEpoch != 4 {
		t.Fatalf("claim after backoff = %+v found=%t err=%v", third, found, err)
	}
	if _, _, err := store.RegisterCodeSessionWorkerAtEpoch(ctx, codeSessionID, second.WorkerEpoch, CodeSessionWorkerBinding{}, 20*time.Second); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("stale epoch register: %v", err)
	}
	if _, _, err := store.RegisterCodeSessionWorkerAtEpoch(ctx, codeSessionID, third.WorkerEpoch, CodeSessionWorkerBinding{}, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCodeSessionWorkerConnectedForEpoch(ctx, codeSessionID, third.WorkerEpoch); err != nil {
		t.Fatal(err)
	}
	var requested sql.NullTime
	var attempts int
	if err := database.QueryRowContext(ctx, `select worker_claim_requested_at, worker_claim_attempts from code_sessions where external_id = $1`, codeSessionID).Scan(&requested, &attempts); err != nil {
		t.Fatal(err)
	}
	if requested.Valid || attempts != 0 {
		t.Fatalf("after register and connect: requested=%v attempts=%d", requested, attempts)
	}

	execClaimTest(t, ctx, database, `update code_sessions set worker_lease_expires_at = now() - interval '5 seconds' where external_id = $1`, codeSessionID)
	if _, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-d")); err != nil || found {
		t.Fatalf("idle session with expired lease claimed: found=%t err=%v", found, err)
	}
	execClaimTest(t, ctx, database, `update sessions set status = 'running' where external_id = 'sesn_claim_1'`)
	if _, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-d")); err != nil || found {
		t.Fatalf("running session claimed within lease grace: found=%t err=%v", found, err)
	}
	if _, err := store.RecordCodeSessionWorkerHeartbeat(ctx, codeSessionID, third.WorkerEpoch, fixedWorkerLease(20*time.Second, 10*time.Second)); err != nil {
		t.Fatalf("heartbeat within grace: %v", err)
	}
	execClaimTest(t, ctx, database, `update code_sessions set worker_lease_expires_at = now() - interval '11 seconds' where external_id = $1`, codeSessionID)
	takeover, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-d"))
	if err != nil || !found || takeover.WorkerEpoch != 5 {
		t.Fatalf("takeover after grace = %+v found=%t err=%v", takeover, found, err)
	}
	if takeover.TraceParent != "" || takeover.RequestedAt != nil {
		t.Fatalf("lease takeover reused dispatch trace=%q requested_at=%v", takeover.TraceParent, takeover.RequestedAt)
	}
	if _, err := store.RecordCodeSessionWorkerHeartbeat(ctx, codeSessionID, third.WorkerEpoch, fixedWorkerLease(20*time.Second, 10*time.Second)); err == nil {
		t.Fatal("stale worker heartbeat accepted after takeover")
	}
}

func TestCodeSessionClaimsPostgresExhaustion(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeSessionID := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	expire := `update code_sessions set worker_claim_expires_at = now() - interval '1 second' where external_id = $1`
	for attempt := 1; attempt <= 3; attempt++ {
		claim, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a"))
		if err != nil || !found || claim.Exhausted {
			t.Fatalf("attempt %d = %+v found=%t err=%v", attempt, claim, found, err)
		}
		execClaimTest(t, ctx, database, expire, codeSessionID)
	}
	exhausted, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a"))
	if err != nil || !found || !exhausted.Exhausted || exhausted.WorkerEpoch != 5 {
		t.Fatalf("exhausted claim = %+v found=%t err=%v", exhausted, found, err)
	}
	if _, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a")); err != nil || found {
		t.Fatalf("claim during exhausted hold: found=%t err=%v", found, err)
	}
	execClaimTest(t, ctx, database, expire, codeSessionID)
	repeated, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a"))
	if err != nil || !found || !repeated.Exhausted || repeated.WorkerEpoch != exhausted.WorkerEpoch {
		t.Fatalf("repeated hold after failed publication = %+v found=%t err=%v", repeated, found, err)
	}
	if err := store.ResetExhaustedCodeSessionClaim(ctx, exhausted.CodeSessionUUID, exhausted.WorkerEpoch-1); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("reset with stale epoch: %v", err)
	}
	if err := store.ResetExhaustedCodeSessionClaim(ctx, exhausted.CodeSessionUUID, exhausted.WorkerEpoch); err != nil {
		t.Fatal(err)
	}
	execClaimTest(t, ctx, database, `update sessions set status = 'idle' where external_id = 'sesn_claim_1'`)
	if _, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a")); err != nil || found {
		t.Fatalf("failed idle session claimed: found=%t err=%v", found, err)
	}
	execClaimTest(t, ctx, database, `update sessions set status = 'running' where external_id = 'sesn_claim_1'`)
	if claim, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a")); err != nil || !found || claim.Exhausted {
		t.Fatalf("claim after new input = %+v found=%t err=%v", claim, found, err)
	}
}

func TestCodeSessionClaimsPostgresExhaustionClearsCrashedRegistration(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeSessionID := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	claim, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a"))
	if err != nil || !found {
		t.Fatalf("claim = %+v found=%t err=%v", claim, found, err)
	}
	if _, _, err := store.RegisterCodeSessionWorkerAtEpoch(ctx, codeSessionID, claim.WorkerEpoch, CodeSessionWorkerBinding{}, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	execClaimTest(t, ctx, database, `update code_sessions
		set worker_lease_expires_at = now() - interval '11 seconds', worker_claim_attempts = 3
		where external_id = $1`, codeSessionID)
	exhausted, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-b"))
	if err != nil || !found || !exhausted.Exhausted || exhausted.WorkerEpoch != claim.WorkerEpoch+1 {
		t.Fatalf("exhausted claim = %+v found=%t err=%v", exhausted, found, err)
	}
	if _, _, err := store.RegisterCodeSessionWorkerAtEpoch(ctx, codeSessionID, claim.WorkerEpoch, CodeSessionWorkerBinding{}, 20*time.Second); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("stale register after exhaustion: %v", err)
	}
	var leaseCleared bool
	if err := database.QueryRowContext(ctx, `select worker_lease_expires_at is null from code_sessions where external_id = $1`, codeSessionID).Scan(&leaseCleared); err != nil || !leaseCleared {
		t.Fatalf("exhaustion kept the crashed worker lease: cleared=%t err=%v", leaseCleared, err)
	}
	if err := store.ResetExhaustedCodeSessionClaim(ctx, exhausted.CodeSessionUUID, exhausted.WorkerEpoch); err != nil {
		t.Fatalf("reset after crashed registration: %v", err)
	}
	execClaimTest(t, ctx, database, `update code_sessions set worker_claim_expires_at = null where external_id = $1`, codeSessionID)
	if recovered, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-b")); err != nil || !found || recovered.Exhausted {
		t.Fatalf("claim after reset = %+v found=%t err=%v", recovered, found, err)
	}
}

func TestCodeSessionClaimsPostgresPermanentFailure(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeSessionID := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	claim, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a"))
	if err != nil || !found {
		t.Fatalf("claim = %+v found=%t err=%v", claim, found, err)
	}
	input := FailCodeSessionClaimInput{CodeSessionExternalID: codeSessionID, WorkerID: "worker-b", WorkerEpoch: claim.WorkerEpoch, MaxAttempts: 3, Hold: time.Minute}
	if _, err := store.FailCodeSessionClaimPermanently(ctx, input); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("permanent failure by another worker: %v", err)
	}
	input.WorkerID = "worker-a"
	failed, err := store.FailCodeSessionClaimPermanently(ctx, input)
	if err != nil || !failed.Exhausted || failed.CodeSessionUUID == "" || failed.WorkerEpoch != claim.WorkerEpoch+1 {
		t.Fatalf("permanent failure = %+v err=%v", failed, err)
	}
	if _, _, err := store.RegisterCodeSessionWorkerAtEpoch(ctx, codeSessionID, claim.WorkerEpoch, CodeSessionWorkerBinding{}, 20*time.Second); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("stale register after permanent failure: %v", err)
	}
	if _, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-b")); err != nil || found {
		t.Fatalf("permanently failed session claimed during hold: found=%t err=%v", found, err)
	}
	execClaimTest(t, ctx, database, `update code_sessions set worker_claim_expires_at = now() - interval '1 second' where external_id = $1`, codeSessionID)
	again, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-b"))
	if err != nil || !found || !again.Exhausted || again.WorkerEpoch != failed.WorkerEpoch {
		t.Fatalf("unreset permanent failure must stay exhausted: %+v found=%t err=%v", again, found, err)
	}
	if err := store.ResetExhaustedCodeSessionClaim(ctx, failed.CodeSessionUUID, failed.WorkerEpoch-1); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("reset with the fenced epoch: %v", err)
	}
	if err := store.ResetExhaustedCodeSessionClaim(ctx, failed.CodeSessionUUID, failed.WorkerEpoch); err != nil {
		t.Fatal(err)
	}
}

func TestCodeSessionClaimsPostgresIdleReleaseClearsLease(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeSessionID := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	claim, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-a"))
	if err != nil || !found {
		t.Fatalf("claim = %+v found=%t err=%v", claim, found, err)
	}
	if _, _, err := store.RegisterCodeSessionWorkerAtEpoch(ctx, codeSessionID, claim.WorkerEpoch, CodeSessionWorkerBinding{}, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	execClaimTest(t, ctx, database, `update sessions set status = 'idle' where external_id = 'sesn_claim_1'`)
	if err := store.ReleaseCodeSessionClaim(ctx, codeSessionID, "worker-b", claim.WorkerEpoch, CodeSessionClaimReleaseIdle); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("idle release by another worker: %v", err)
	}
	if err := store.ReleaseCodeSessionClaim(ctx, codeSessionID, "worker-a", claim.WorkerEpoch, CodeSessionClaimReleaseIdle); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-b")); err != nil || found {
		t.Fatalf("idle released session claimed without input: found=%t err=%v", found, err)
	}
	execClaimTest(t, ctx, database, `update sessions set status = 'running' where external_id = 'sesn_claim_1'`)
	next, found, err := store.ClaimNextCodeSession(ctx, claimTestInput("worker-b"))
	if err != nil || !found || next.WorkerEpoch != claim.WorkerEpoch+1 {
		t.Fatalf("claim after idle release = %+v found=%t err=%v", next, found, err)
	}
}

func fixedWorkerLease(ttl, grace time.Duration) WorkerLeasePolicy {
	return func(string) WorkerLeaseTimings { return WorkerLeaseTimings{TTL: ttl, Grace: grace} }
}

func claimAndRegister(t *testing.T, ctx context.Context, store *DB, codeSessionID, workerID string) int64 {
	t.Helper()
	claim, found, err := store.ClaimNextCodeSession(ctx, claimTestInput(workerID))
	if err != nil || !found || claim.CodeSessionExternalID != codeSessionID {
		t.Fatalf("claim = %+v found=%t err=%v", claim, found, err)
	}
	if _, _, err := store.RegisterCodeSessionWorkerAtEpoch(ctx, codeSessionID, claim.WorkerEpoch, CodeSessionWorkerBinding{}, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	return claim.WorkerEpoch
}

func TestCodeSessionClaimsPostgresWorkerClaimStateReportsReleasedAndSupersededEpochs(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	if err := store.ValidateCodeSessionWorkerClaim(ctx, claimTestOrganization, claimTestWorkspace, "cse_missing", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing code session: %v", err)
	}
	for index, kind := range []CodeSessionClaimReleaseKind{CodeSessionClaimReleaseIdle, CodeSessionClaimReleaseHandoff, CodeSessionClaimReleaseRetry} {
		codeSessionID := seedClaimCodeSession(t, ctx, database, index+1, "claim", "running")
		owner, successor := fmt.Sprintf("worker-a-%d", index), fmt.Sprintf("worker-b-%d", index)
		epoch := claimAndRegister(t, ctx, store, codeSessionID, owner)
		if err := store.ValidateCodeSessionWorkerClaim(ctx, claimTestOrganization, "53000000-0000-0000-0000-0000000000ff", codeSessionID, epoch); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: other workspace read the claim: %v", kind, err)
		}
		if err := store.MarkCodeSessionWorkerDisconnectedForEpoch(ctx, codeSessionID, epoch); err != nil {
			t.Fatal(err)
		}
		if err := store.ReleaseCodeSessionClaim(ctx, codeSessionID, owner, epoch, kind); err != nil {
			t.Fatal(err)
		}
		if err := store.ValidateCodeSessionWorkerClaim(ctx, claimTestOrganization, claimTestWorkspace, codeSessionID, epoch); !errors.Is(err, ErrWorkerClaimReleased) {
			t.Fatalf("%s release at the same epoch: %v", kind, err)
		}
		execClaimTest(t, ctx, database, `update code_sessions set worker_claim_expires_at = NOW() - INTERVAL '1 second' where external_id = $1`, codeSessionID)
		next := claimAndRegister(t, ctx, store, codeSessionID, successor)
		if err := store.ValidateCodeSessionWorkerClaim(ctx, claimTestOrganization, claimTestWorkspace, codeSessionID, epoch); !errors.Is(err, ErrWorkerEpochMismatch) {
			t.Fatalf("%s superseded epoch %d (current %d): %v", kind, epoch, next, err)
		}
		execClaimTest(t, ctx, database, `update code_sessions set status = 'terminated' where external_id = $1`, codeSessionID)
	}
}

func TestCodeSessionClaimsPostgresWorkerClaimStateKeepsRegisteredEpoch(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	legacy := seedClaimCodeSession(t, ctx, database, 1, "nats", "running")
	if err := store.ValidateCodeSessionWorkerClaim(ctx, claimTestOrganization, claimTestWorkspace, legacy, 1); err != nil {
		t.Fatalf("unclaimed transport: %v", err)
	}
	codeSessionID := seedClaimCodeSession(t, ctx, database, 2, "claim", "running")
	epoch := claimAndRegister(t, ctx, store, codeSessionID, "worker-a")
	for _, step := range []func() error{
		func() error { return nil },
		func() error { return store.MarkCodeSessionWorkerDisconnectedForEpoch(ctx, codeSessionID, epoch) },
		func() error {
			_, err := store.HeartbeatCodeSessionWorker(ctx, codeSessionID, epoch, 20*time.Second)
			return err
		},
		func() error { return store.MarkCodeSessionWorkerConnectedForEpoch(ctx, codeSessionID, epoch) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
		if err := store.ValidateCodeSessionWorkerClaim(ctx, claimTestOrganization, claimTestWorkspace, codeSessionID, epoch); err != nil {
			t.Fatalf("registered epoch lost its claim: %v", err)
		}
	}
}
